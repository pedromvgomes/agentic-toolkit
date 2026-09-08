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
	// merge base by the caller. It is what the diff, the manifest and the
	// convention documents are read at.
	Base string
	// BaseLabel is what the review names the base — the ref the caller named,
	// before it was resolved.
	//
	// Separate from Base because they are read by different audiences. A
	// merge base is a commit id, which is what the work needs and the last
	// thing a person recognises: a reader who typed `--base origin/main`
	// wants that back, and gets a bare forty-character hash if the report
	// renders what the diff was anchored to. Empty means the resolved commit
	// stands in for itself.
	BaseLabel string
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
	// Binary pins the executable instead of resolving a provider on PATH.
	Binary string
	// Preview asks Prepare to classify the reviewed tree without writing it.
	// Only a caller that will start no run may set it: the paths it reports
	// are real, and nothing is behind them.
	Preview bool

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
	// MissingConventions are documents the manifest named that the base ref
	// does not hold.
	MissingConventions []string
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

	// A preview classifies the tree without writing it: Prepare is the seam
	// --dry-run uses, and it starts no process that would read the bytes.
	build := BuildRoot
	if opts.Preview {
		build = PlanRoot
	}
	root, err := build(opts.Dir, opts.Head)
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

	// A manifest that names its own documents has said where its rules live,
	// so one that is missing at the base ref is a misconfiguration to report
	// rather than a default that happens not to exist.
	nominated := len(m.Conventions) > 0
	conventions, missing := readConventions(opts.Dir, opts.Base, m.ConventionDocs(DefaultConventionDocs), nominated)

	material := Material{
		ChangedFiles: names,
		Patch:        patch,
		Conventions:  conventions,
		Root:         root,
		Range:        rangeLabel(opts.baseLabel(), opts.Head),
	}

	plan := &Plan{
		Panel:              sel.Panel,
		Manifest:           manifestLabel(manifestPath, builtin),
		Range:              material.Range,
		Material:           material,
		MissingConventions: missing,
	}
	panel := m.Panels[sel.Panel]
	judgeBody, err := runnerBody(opts.Dir, opts.Base, *m.Judge)
	if err != nil {
		_ = root.Close()
		return nil, nil, nil, nil, err
	}
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

	// The judge is planned, not just the reviewers. It runs once whenever the
	// panel answers, so a preview that counted only reviewers would understate
	// every real review by at least one run. Validators cannot be counted in
	// advance — there is one per distinct candidate finding, and how many
	// there are is what the reviewers have not been asked yet.
	plan.Runs = append(plan.Runs, PlannedRun{
		Label:    "judge",
		Role:     RoleJudge,
		Provider: m.Judge.Provider,
		Model:    m.Judge.Model,
		Prompt:   material.composeWith(judgeBody, "\n---\n\n# The candidate findings\n\n(supplied once the reviewers have answered)\n", judgeInjectionClause),
	})
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
		Panel:              plan.Panel,
		Manifest:           plan.Manifest,
		Range:              plan.Range,
		Skipped:            root.Skipped,
		Conventions:        plan.Material.ConventionPaths(),
		MissingConventions: plan.MissingConventions,
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
		// candidate finding set as one: nothing reconciled it, and an unreconciled
		// pile that looks like a verdict is the failure the judge exists to
		// prevent.
		out.Reason = "this manifest declares no judge, so nothing decided which findings survive"
		out.CostUSD = totalCost(out.Reports)
		return out, nil
	}

	judged, good, discarded, reattached, judgeReport := runJudge(ctx, opts, inv, sched, *m.Judge, plan.Material, kept)
	out.Reports = append(out.Reports, judgeReport)
	out.DiscardedIDs = discarded
	out.ReattachedIDs = reattached
	out.CostUSD = totalCost(out.Reports)

	if !judgeReport.Report.Available {
		// A failing judge makes the whole review unavailable, where a failing
		// reviewer only makes it partial. Nothing decided what survives, and
		// printing the raw candidate finding set as though it had would present an
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

	var jobs []scheduled
	for _, planned := range plan.Runs {
		if planned.Role != RoleReviewer {
			continue
		}
		runner := m.Reviewers[reviewerOf(planned.Label)]
		jobs = append(jobs, scheduled{
			runner: runner,
			proto: RunReport{
				Label: planned.Label, Role: RoleReviewer,
				Provider: runner.Provider, Model: runner.Model,
			},
			fn: reviewerJob(opts, inv, runner, planned, plan.Material),
		})
	}

	reports := sched.runAll(ctx, jobs)

	// Findings are folded on path, category and normalised evidence, and
	// Corroboration is the number of distinct runs that reached the same
	// claim — one flat count, not a per-reviewer one.
	//
	// Category is part of that identity, so agreement is in practice between
	// instances of one reviewer rather than across axes: two axes describing
	// one defect usually file it under different categories and stay separate
	// claims. Reconciling those is the judge's job, which is why it is handed
	// the whole set rather than a pre-merged one.
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

// runValidators puts each distinct candidate finding to a validator.
//
// Per distinct candidate finding rather than per instance: the same claim reached by
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
		// Without a body there is nothing to ask, so every candidate finding goes
		// forward unvalidated rather than being dropped by a run that never
		// happened.
		return candidates, []RunReport{{
			Label: "validator", Role: RoleValidator,
			Provider: validator.Provider, Model: validator.Model,
			Report: Unavailable("the validator prompt could not be read: %v", err),
		}}
	}

	jobs := make([]scheduled, 0, len(candidates))
	// Each job records the candidate finding it was made for. A positional
	// correspondence between jobs and candidate findings would be one `continue` away
	// from applying a verdict to a different finding, and a misattributed
	// rejection drops a claim nobody judged.
	forCandidate := make([]int, 0, len(candidates))
	for i := range candidates {
		f := candidates[i]
		// A prompt-injection finding is not put to a validator at all. The
		// validator's bar is a code defect it can independently reproduce, and
		// an imperative planted in a diff is none of those things — so asking
		// costs a run whose only available answer is "rejected", and a
		// rejection here drops the finding before the judge ever sees it.
		// Its quote is the verification.
		if f.Injected() {
			continue
		}
		forCandidate = append(forCandidate, i)
		label := "validator:" + f.ID
		jobs = append(jobs, scheduled{
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
		v, ok := verdictOf(r)
		if !ok {
			continue
		}
		c := forCandidate[i]
		out[c].Verdict = v
		// A downgrade is a severity the validator argued for, so it is
		// applied here rather than left for the judge to rediscover.
		if v.Verdict == VerdictDowngraded && v.Severity.Valid() {
			out[c].Severity = v.Severity
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
		prompt := material.composeWith(body,
			"\n---\n\n# The finding to verify\n\n"+renderCandidateFinding(f, false), validatorInjectionClause)
		req, err := request(validator, prompt, validatorSchema, material.Root, timeoutOf(opts))
		if err != nil {
			out.Report = Unavailable("%s could not be prepared: %v", label, err)
			return out
		}
		res, err := inv.Invoke(ctx, validator, req)
		raw, report := classify(res, err, label)
		out.CostUSD = res.Usage.CostUSD
		if res.Model != "" {
			out.Model = res.Model
		}
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

// runJudge puts the surviving candidate findings to the judge and re-attaches
// what the judge does not return.
func runJudge(ctx context.Context, opts Options, inv invoker, sched *scheduler,
	judge review.Runner, material Material, candidates []Finding) ([]Finding, []string, []string, []string, RunReport) {

	out := RunReport{Label: "judge", Role: RoleJudge, Provider: judge.Provider, Model: judge.Model}

	if len(candidates) == 0 {
		// Nothing to reconcile. Spending a run to be told that an empty set
		// stays empty is a run that can only fail.
		out.Report = Answered(nil)
		return nil, nil, nil, nil, out
	}

	body, err := builtinPromptFor(opts, judge)
	if err != nil {
		out.Report = Unavailable("the judge prompt could not be read: %v", err)
		return nil, nil, nil, nil, out
	}

	prompt := material.composeWith(body,
		"\n---\n\n# The candidate findings\n\n"+renderCandidateFindings(candidates), judgeInjectionClause)
	req, err := request(judge, prompt, judgeSchema, material.Root, timeoutOf(opts))
	if err != nil {
		out.Report = Unavailable("the judge run could not be prepared: %v", err)
		return nil, nil, nil, nil, out
	}

	// One judge run, so it takes its slot directly rather than through the
	// batch helper: the judge's answer is not a finding set, and routing it
	// through a shape that carries one would mean packing raw JSON into a
	// field named for quoted code.
	release, err := sched.acquire(ctx, judge)
	if err != nil {
		out.Report = Unavailable("the judge was never started: %v", err)
		return nil, nil, nil, nil, out
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
		return nil, nil, nil, nil, out
	}

	findings, good, discarded, reattached, err := applyJudgement(raw, candidates)
	if err != nil {
		out.Report = Unavailable("read the judge's answer: %v", err)
		return nil, nil, nil, nil, out
	}
	out.Report = Answered(findings)
	return findings, good, discarded, reattached, out
}

// applyJudgement re-attaches what the judge did not return.
//
// The judge answers with ids, a severity and prose. file, line, category and
// evidence come from the candidate finding that was issued the id, byte for byte —
// evidence above all, because a fingerprint is the quoted code hashed, and a
// judge that tidied a quote would silently repost a finding somebody had
// already resolved. See ADR 0008.
func applyJudgement(raw []byte, candidates []Finding) (findings []Finding, good []string, discarded, reattached []string, err error) {
	var answer judgeAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, nil, nil, nil, err
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

	// A prompt-injection candidate finding the judge did not return is re-attached at
	// the severity it arrived with.
	//
	// This is the one place the judge's authority stops. Everywhere else it
	// narrows freely, which is what it is for; here a dropped finding converts
	// an injected instruction into a clean review, and a clean review is what
	// unblocks approval — the conversion ADR 0007 exists to prevent. Leaving
	// it to the prompt would make the guarantee a sentence the judge has to
	// have read, rather than a property of the code.
	for _, f := range candidates {
		if f.Injected() && !seen[f.ID] {
			seen[f.ID] = true
			findings = append(findings, f)
			reattached = append(reattached, f.ID)
		}
	}

	sort.Strings(discarded)
	sort.Strings(reattached)
	return findings, answer.Good, discarded, reattached, nil
}

// renderCandidateFindings lays the candidate findings out for the judge.
func renderCandidateFindings(candidates []Finding) string {
	var b strings.Builder
	for _, f := range candidates {
		b.WriteString(renderCandidateFinding(f, true))
		b.WriteString("\n")
	}
	return b.String()
}

// renderCandidateFinding lays one candidate finding out. withID is false for a
// validator, which is judging a claim rather than answering about an
// identified one.
func renderCandidateFinding(f Finding, withID bool) string {
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
	writeFenced(&b, "- evidence:\n\n", "", f.Evidence)
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

// assignIDs gives every candidate finding the id the judge will answer with.
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

// baseLabel is what the review names the base: the ref the caller named, or
// the resolved commit when they named none.
func (o Options) baseLabel() string {
	if o.BaseLabel != "" {
		return o.BaseLabel
	}
	return o.Base
}

// rangeLabel renders what the change was measured over.
func rangeLabel(base, head string) string {
	if head == "" {
		return base + "...working tree"
	}
	return base + "..." + head
}
