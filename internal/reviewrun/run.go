// Package reviewrun runs a review: it assembles the prompts, writes the review
// root, runs the panel at quorum, validates what they found and puts the
// survivors to the judge.
//
// It is the only package in code-review that invokes a model. internal/review
// stays reachable without one — that is what makes `agtk code-review explain`
// free to run on a hook — and this package imports it one way.
//
// Nothing here talks to GitHub. The judge decides what the review says and
// agtk transmits it, which is ADR 0006's separation, and no credential reaches
// a model's process.
package reviewrun

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

// DefaultTimeout bounds one run. A reviewer reads a diff, follows it into the
// tree and writes findings, so the bound is generous; a run killed early
// reports as one that could not answer rather than as one that found nothing.
const DefaultTimeout = 10 * time.Minute

// Options configure one review.
type Options struct {
	// Dir is the repository the change lives in.
	Dir string
	// Base is the ref the change is measured against, already resolved to a
	// merge base by the caller.
	Base string
	// Head is the ref the change ends at, or "" for the working tree.
	Head string
	// Context is what the review runs against.
	Context review.Context
	// Panel overrides the panel the rules would choose.
	Panel string
	// Timeout bounds one run.
	Timeout time.Duration
	// MaxParallel bounds how many runs are in flight at once.
	MaxParallel int
	// DryRun assembles everything and starts no process.
	DryRun bool
	// Binary pins the executable instead of resolving a provider on PATH.
	Binary string

	// invoker is the seam the driver is reached through. Nil means the real
	// one; tests supply their own.
	invoker invoker
}

// Plan is what a review would do, without doing it.
type Plan struct {
	Panel    string
	Manifest string
	Range    string
	Material Material
	Runs     []PlannedRun
}

// PlannedRun is one run a review would make.
type PlannedRun struct {
	Label    string
	Role     string
	Provider string
	Model    string
	Prompt   string
}

// Prepare works out everything a review needs and starts no process.
//
// Shared by the review and by --dry-run, so what a preview prints is what a
// run would send rather than a second rendering of it that could drift.
//
// The caller closes the returned root.
func Prepare(opts Options) (*Plan, *review.Manifest, *review.Selection, *Root, error) {
	m, manifestPath, builtin, err := loadManifest(opts)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if err := review.CheckCapabilities(manifestLabel(manifestPath, builtin), m); err != nil {
		return nil, nil, nil, nil, err
	}

	profile, err := review.BuildProfile(review.ProfileOptions{
		Dir:  opts.Dir,
		Base: opts.Base,
		Head: opts.Head,
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	sel, err := review.Select(m, opts.Context, profile, opts.Panel)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	root, err := BuildRoot(opts.Dir, opts.Head)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	reviewable := profile.ReviewableFiles()
	names := make([]string, 0, len(reviewable))
	for _, f := range reviewable {
		names = append(names, f.Path)
	}
	patch, err := review.Patch(opts.Dir, opts.Base, opts.Head, reviewable)
	if err != nil {
		_ = root.Close()
		return nil, nil, nil, nil, err
	}

	material := Material{
		ChangedFiles: names,
		Patch:        patch,
		Conventions:  readConventions(opts.Dir, opts.Base, m.ConventionDocs(DefaultConventionDocs)),
		Root:         root,
		Range:        rangeLabel(opts.Base, opts.Head),
	}

	plan := &Plan{
		Panel:    sel.Panel,
		Manifest: manifestLabel(manifestPath, builtin),
		Range:    material.Range,
		Material: material,
	}
	panel := m.Panels[sel.Panel]
	for _, name := range panel.Reviewers {
		runner := m.Reviewers[name]
		body, err := runnerBody(opts.Dir, opts.Base, runner)
		if err != nil {
			_ = root.Close()
			return nil, nil, nil, nil, err
		}
		prompt := material.compose(body)
		for i := 1; i <= panel.EffectiveQuorum(); i++ {
			plan.Runs = append(plan.Runs, PlannedRun{
				Label:    instanceLabel(name, i, panel.EffectiveQuorum()),
				Role:     RoleReviewer,
				Provider: runner.Provider,
				Model:    runner.Model,
				Prompt:   prompt,
			})
		}
	}
	return plan, m, sel, root, nil
}

// Run reviews the change and returns what survived.
func Run(ctx context.Context, opts Options) (*Review, error) {
	plan, m, sel, root, err := Prepare(opts)
	if err != nil {
		return nil, err
	}
	// Removed on every exit path including cancellation: the root is written
	// into the system temporary directory, and an interrupted review has no
	// later opportunity to tidy up after itself.
	defer func() { _ = root.Close() }()

	inv := opts.invoker
	if inv == nil {
		inv = driverInvoker{binary: opts.Binary}
	}
	sched := newScheduler(opts.MaxParallel, inv.Limit)

	out := &Review{
		Panel:       plan.Panel,
		Manifest:    plan.Manifest,
		Range:       plan.Range,
		Skipped:     root.Skipped,
		Conventions: plan.Material.ConventionPaths(),
	}

	candidates, reports := runReviewers(ctx, opts, inv, sched, m, plan)
	out.Reports = append(out.Reports, reports...)

	// A panel where nothing answered has looked at nothing, and an empty
	// finding set from it is the absence of a review rather than a clean one.
	// Left unguarded this is the worst failure the whole design is arranged
	// against: every reviewer fails, the judge is skipped because there is
	// nothing to reconcile, and the run prints "no findings" — which is what
	// unblocks approval.
	if !anyReviewerAnswered(reports) {
		out.Reason = "no reviewer answered, so nothing looked at this change"
		out.CostUSD = totalCost(out.Reports)
		return out, nil
	}

	if sel.Validates && m.Validator != nil {
		var validatorReports []RunReport
		candidates, validatorReports = runValidators(ctx, opts, inv, sched, *m.Validator, plan.Material, candidates)
		out.Reports = append(out.Reports, validatorReports...)
	}

	kept := make([]Finding, 0, len(candidates))
	for _, f := range candidates {
		if f.Upheld() {
			kept = append(kept, f)
		} else {
			out.DroppedByValidator++
		}
	}

	if m.Judge == nil {
		// A manifest cannot omit the judge — the parser refuses one that does
		// — so reaching here means the manifest was built in code and is
		// inconsistent. Reported as no verdict rather than by presenting the
		// candidate set as one: nothing reconciled it, and an unreconciled
		// pile that looks like a verdict is the failure the judge exists to
		// prevent.
		out.Reason = "this manifest declares no judge, so nothing decided which findings survive"
		out.CostUSD = totalCost(out.Reports)
		return out, nil
	}

	judged, good, discarded, judgeReport := runJudge(ctx, opts, inv, sched, *m.Judge, plan.Material, kept)
	out.Reports = append(out.Reports, judgeReport)
	out.DiscardedIDs = discarded
	out.CostUSD = totalCost(out.Reports)

	if !judgeReport.Report.Available {
		// A failing judge makes the whole review unavailable, where a failing
		// reviewer only makes it partial. Nothing decided what survives, and
		// printing the raw candidate set as though it had would present an
		// unreconciled pile as a verdict.
		out.Reason = judgeReport.Report.Reason
		return out, nil
	}

	out.Findings = judged
	out.Good = good
	sortFindings(out.Findings)
	out.Available = true
	return out, nil
}

// anyReviewerAnswered reports whether at least one reviewer produced findings
// to reason about — an empty list included, which is a reviewer saying it
// looked and found nothing.
func anyReviewerAnswered(reports []RunReport) bool {
	for _, r := range reports {
		if r.Role == RoleReviewer && r.Report.Available {
			return true
		}
	}
	return false
}

// runReviewers runs the panel at quorum and folds the instances into one set
// of distinct claims.
func runReviewers(ctx context.Context, opts Options, inv invoker, sched *scheduler,
	m *review.Manifest, plan *Plan) ([]Finding, []RunReport) {

	var jobs []job
	for _, planned := range plan.Runs {
		runner := m.Reviewers[reviewerOf(planned.Label)]
		jobs = append(jobs, job{
			runner: runner,
			proto: RunReport{
				Label: planned.Label, Role: RoleReviewer,
				Provider: runner.Provider, Model: runner.Model,
			},
			fn: reviewerJob(opts, inv, runner, planned, plan.Material),
		})
	}

	reports := sched.runAll(ctx, jobs)

	// Corroboration counts distinct instances, so the instances are folded
	// per reviewer name and then across reviewers: two instances of one
	// reviewer agreeing is a weaker signal than two different axes agreeing,
	// and both are stronger than one voice.
	instances := make([][]Finding, 0, len(reports))
	for _, r := range reports {
		if findings, ok := r.Report.Findings(); ok {
			instances = append(instances, findings)
		}
	}
	return assignIDs(corroborate(instances)), reports
}

// reviewerJob makes one reviewer instance's run.
func reviewerJob(opts Options, inv invoker, runner review.Runner, planned PlannedRun, material Material) func(context.Context) RunReport {
	return func(ctx context.Context) RunReport {
		out := RunReport{
			Label: planned.Label, Role: RoleReviewer,
			Provider: runner.Provider, Model: runner.Model,
		}
		req, err := request(runner, planned.Prompt, findingSchema, material.Root, timeoutOf(opts))
		if err != nil {
			out.Report = Unavailable("%s could not be prepared: %v", planned.Label, err)
			return out
		}
		res, err := inv.Invoke(ctx, runner, req)
		raw, report := classify(res, err, planned.Label)
		out.CostUSD = res.Usage.CostUSD
		if res.Model != "" {
			out.Model = res.Model
		}
		if !report.Available {
			out.Report = report
			return out
		}
		findings, err := decodeFindings(raw, reviewerOf(planned.Label))
		if err != nil {
			out.Report = Unavailable("%v", err)
			return out
		}
		out.Report = Answered(findings)
		return out
	}
}

// runValidators puts each distinct candidate to a validator.
//
// Per distinct candidate rather than per instance: the same claim reached by
// three instances is one claim to verify, and validating it three times would
// spend three runs to learn one thing.
//
// Each validator sees one finding and never the set. Seeing the set is the
// judge's job, and a validator that could see it would be reconciling rather
// than verifying — which is the whole of what an independent second opinion
// adds.
func runValidators(ctx context.Context, opts Options, inv invoker, sched *scheduler,
	validator review.Runner, material Material, candidates []Finding) ([]Finding, []RunReport) {

	if len(candidates) == 0 {
		return candidates, nil
	}
	body, err := builtinPromptFor(opts, validator)
	if err != nil {
		// Without a body there is nothing to ask, so every candidate goes
		// forward unvalidated rather than being dropped by a run that never
		// happened.
		return candidates, []RunReport{{
			Label: "validator", Role: RoleValidator,
			Provider: validator.Provider, Model: validator.Model,
			Report: Unavailable("the validator prompt could not be read: %v", err),
		}}
	}

	jobs := make([]job, 0, len(candidates))
	for i := range candidates {
		f := candidates[i]
		label := "validator:" + f.ID
		jobs = append(jobs, job{
			runner: validator,
			proto: RunReport{
				Label: label, Role: RoleValidator,
				Provider: validator.Provider, Model: validator.Model,
			},
			fn: validatorJob(opts, inv, validator, material, body, f, label),
		})
	}
	reports := sched.runAll(ctx, jobs)

	out := make([]Finding, len(candidates))
	copy(out, candidates)
	for i, r := range reports {
		if v, ok := verdictOf(r); ok {
			out[i].Verdict = v
			// A downgrade is a severity the validator argued for, so it is
			// applied here rather than left for the judge to rediscover.
			if v.Verdict == VerdictDowngraded && v.Severity.Valid() {
				out[i].Severity = v.Severity
			}
		}
	}
	return out, reports
}

// verdictOf reads a validator's answer out of its report.
func verdictOf(r RunReport) (*Verdict, bool) {
	raw, ok := r.Report.Findings()
	if !ok || len(raw) != 1 {
		return nil, false
	}
	return raw[0].Verdict, raw[0].Verdict != nil
}

// validatorJob puts one finding to the validator.
func validatorJob(opts Options, inv invoker, validator review.Runner, material Material,
	body string, f Finding, label string) func(context.Context) RunReport {

	return func(ctx context.Context) RunReport {
		out := RunReport{
			Label: label, Role: RoleValidator,
			Provider: validator.Provider, Model: validator.Model,
		}
		prompt := material.compose(body) + "\n\n---\n\n# The finding to verify\n\n" + renderCandidate(f, false)
		req, err := request(validator, prompt, validatorSchema, material.Root, timeoutOf(opts))
		if err != nil {
			out.Report = Unavailable("%s could not be prepared: %v", label, err)
			return out
		}
		res, err := inv.Invoke(ctx, validator, req)
		raw, report := classify(res, err, label)
		out.CostUSD = res.Usage.CostUSD
		if !report.Available {
			out.Report = report
			return out
		}
		var v Verdict
		if err := json.Unmarshal(raw, &v); err != nil {
			out.Report = Unavailable("read %s's answer: %v", label, err)
			return out
		}
		// A finding carrying only the verdict, so verdictOf can read it back
		// through the same Report shape every other run reports through.
		out.Report = Answered([]Finding{{ID: f.ID, Verdict: &v}})
		return out
	}
}

// runJudge puts the survivors to the judge and re-attaches what the judge does
// not return.
func runJudge(ctx context.Context, opts Options, inv invoker, sched *scheduler,
	judge review.Runner, material Material, candidates []Finding) ([]Finding, []string, []string, RunReport) {

	out := RunReport{Label: "judge", Role: RoleJudge, Provider: judge.Provider, Model: judge.Model}

	if len(candidates) == 0 {
		// Nothing to reconcile. Spending a run to be told that an empty set
		// stays empty is a run that can only fail.
		out.Report = Answered(nil)
		return nil, nil, nil, out
	}

	body, err := builtinPromptFor(opts, judge)
	if err != nil {
		out.Report = Unavailable("the judge prompt could not be read: %v", err)
		return nil, nil, nil, out
	}

	prompt := material.compose(body) + "\n\n---\n\n# The candidate findings\n\n" + renderCandidates(candidates)
	req, err := request(judge, prompt, judgeSchema, material.Root, timeoutOf(opts))
	if err != nil {
		out.Report = Unavailable("the judge run could not be prepared: %v", err)
		return nil, nil, nil, out
	}

	// One judge run, so it takes its slot directly rather than through the
	// batch helper: the judge's answer is not a finding set, and routing it
	// through a shape that carries one would mean packing raw JSON into a
	// field named for quoted code.
	release, err := sched.acquire(ctx, judge)
	if err != nil {
		out.Report = Unavailable("the judge was never started: %v", err)
		return nil, nil, nil, out
	}
	res, invokeErr := inv.Invoke(ctx, judge, req)
	release()

	raw, report := classify(res, invokeErr, "the judge")
	out.CostUSD = res.Usage.CostUSD
	if res.Model != "" {
		out.Model = res.Model
	}
	if !report.Available {
		out.Report = report
		return nil, nil, nil, out
	}

	findings, good, discarded, err := applyJudgement(raw, candidates)
	if err != nil {
		out.Report = Unavailable("read the judge's answer: %v", err)
		return nil, nil, nil, out
	}
	out.Report = Answered(findings)
	return findings, good, discarded, out
}

// applyJudgement re-attaches what the judge did not return.
//
// The judge answers with ids, a severity and prose. file, line, category and
// evidence come from the candidate that was issued the id, byte for byte —
// evidence above all, because a fingerprint is the quoted code hashed, and a
// judge that tidied a quote would silently repost a finding somebody had
// already resolved. See ADR 0008.
func applyJudgement(raw []byte, candidates []Finding) (findings []Finding, good []string, discarded []string, err error) {
	var answer judgeAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, nil, nil, err
	}

	byID := make(map[string]Finding, len(candidates))
	for _, f := range candidates {
		byID[f.ID] = f
	}

	seen := map[string]bool{}
	for _, j := range answer.Findings {
		original, known := byID[j.ID]
		if !known {
			// An id nobody issued has no evidence behind it. Discarding is
			// the only honest reading: trusting it posts a finding with no
			// code under it, and failing the run throws away a panel that has
			// already been paid for because of one invented label.
			discarded = append(discarded, j.ID)
			continue
		}
		if seen[j.ID] {
			continue
		}
		seen[j.ID] = true

		f := original
		if j.Severity.Valid() {
			f.Severity = j.Severity
		}
		if strings.TrimSpace(j.Issue) != "" {
			f.Issue = j.Issue
		}
		if strings.TrimSpace(j.Suggestion) != "" {
			f.Suggestion = j.Suggestion
		}
		findings = append(findings, f)
	}
	sort.Strings(discarded)
	return findings, answer.Good, discarded, nil
}

// renderCandidates lays the candidate set out for the judge.
func renderCandidates(candidates []Finding) string {
	var b strings.Builder
	for _, f := range candidates {
		b.WriteString(renderCandidate(f, true))
		b.WriteString("\n")
	}
	return b.String()
}

// renderCandidate lays one candidate out. withID is false for a validator,
// which is judging a claim rather than answering about an identified one.
func renderCandidate(f Finding, withID bool) string {
	var b strings.Builder
	if withID {
		fmt.Fprintf(&b, "## %s\n\n", f.ID)
	}
	fmt.Fprintf(&b, "- reviewer: %s\n", f.Reviewer)
	fmt.Fprintf(&b, "- file: %s\n", f.Path)
	fmt.Fprintf(&b, "- lines: %s\n", lineRange(f))
	fmt.Fprintf(&b, "- category: %s\n", f.Category)
	fmt.Fprintf(&b, "- severity: %s\n", f.Severity)
	fmt.Fprintf(&b, "- confidence: %s\n", f.Confidence)
	if withID {
		fmt.Fprintf(&b, "- reached independently by: %d reviewer instance(s)\n", f.Corroboration)
		if f.Verdict != nil {
			fmt.Fprintf(&b, "- validator: %s — %s\n", f.Verdict.Verdict, f.Verdict.Reason)
		} else {
			fmt.Fprintf(&b, "- validator: did not run\n")
		}
	}
	fmt.Fprintf(&b, "- issue: %s\n", f.Issue)
	fmt.Fprintf(&b, "- evidence:\n\n```\n%s\n```\n", strings.TrimRight(f.Evidence, "\n"))
	if f.Suggestion != "" {
		fmt.Fprintf(&b, "- suggested: %s\n", f.Suggestion)
	}
	return b.String()
}

// lineRange renders a finding's location.
func lineRange(f Finding) string {
	if f.StartLine == nil {
		return "cross-cutting (no line)"
	}
	if f.EndLine == nil || *f.EndLine == *f.StartLine {
		return strconv.Itoa(*f.StartLine)
	}
	return strconv.Itoa(*f.StartLine) + "-" + strconv.Itoa(*f.EndLine)
}

// assignIDs gives every candidate the id the judge will answer with.
//
// Sequential and per-run. They mean nothing outside one review and are never
// persisted: identity across runs is the fingerprint.
func assignIDs(findings []Finding) []Finding {
	for i := range findings {
		findings[i].ID = "f" + strconv.Itoa(i+1)
	}
	return findings
}

// instanceLabel names one run of a reviewer, numbering it only when a quorum
// makes the number meaningful.
func instanceLabel(name string, instance, quorum int) string {
	if quorum <= 1 {
		return name
	}
	return name + "#" + strconv.Itoa(instance)
}

// reviewerOf recovers the manifest name from an instance label.
func reviewerOf(label string) string {
	if i := strings.IndexByte(label, '#'); i >= 0 {
		return label[:i]
	}
	return label
}

// timeoutOf is the bound one run gets.
func timeoutOf(opts Options) time.Duration {
	if opts.Timeout > 0 {
		return opts.Timeout
	}
	return DefaultTimeout
}

// totalCost sums what every run spent.
func totalCost(reports []RunReport) float64 {
	var total float64
	for _, r := range reports {
		total += r.CostUSD
	}
	return total
}

// loadManifest reads the rules this review is run under.
//
// A context that posts reads them from the base ref: everything on the branch
// is written by its author, so a manifest read from the head would let a
// change name the reviewers that judge it.
func loadManifest(opts Options) (*review.Manifest, string, bool, error) {
	if opts.Context.Posts() {
		return review.LoadAtRef(opts.Dir, opts.Base)
	}
	return review.Load(opts.Dir)
}

// builtinPromptFor reads a judge's or validator's body.
func builtinPromptFor(opts Options, r review.Runner) (string, error) {
	return runnerBody(opts.Dir, opts.Base, r)
}

// manifestLabel names which manifest was read.
func manifestLabel(path string, builtin bool) string {
	if builtin {
		return "built-in default"
	}
	return path
}

// rangeLabel renders what the change was measured over.
func rangeLabel(base, head string) string {
	if head == "" {
		return base + "...working tree"
	}
	return base + "..." + head
}
