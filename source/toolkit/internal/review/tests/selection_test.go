package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/review"
)

// profile builds a change profile directly, so selection is tested without a
// repository, a diff or a process.
func profile(files, lines int, refs review.Count, signals ...review.Signal) *review.Profile {
	set := review.NewSignalSet()
	for _, s := range signals {
		set.Add(s)
	}
	p := &review.Profile{ChangedFiles: files, ChangedLines: lines, Signals: set, ReferencingFiles: refs}
	for i := 0; i < files; i++ {
		p.Files = append(p.Files, review.ChangedFile{
			DiffFile: review.DiffFile{Path: "internal/resolver/f.go", Added: 1},
			Language: review.LangGo,
		})
	}
	return p
}

// withPaths replaces a profile's paths, keeping its counts: a test about
// which paths were touched should not silently also change how many.
func withPaths(p *review.Profile, paths ...string) *review.Profile {
	p.Files = nil
	for _, path := range paths {
		p.Files = append(p.Files, review.ChangedFile{
			DiffFile: review.DiffFile{Path: path, Added: 1},
			Language: review.LanguageOf(path),
		})
	}
	return p
}

func selectPanel(t *testing.T, m *review.Manifest, ctx review.Context, p *review.Profile, override string) *review.Selection {
	t.Helper()
	sel, err := review.Select(m, ctx, p, override)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	return sel
}

// With nothing firing, a context runs the panel it declared.
func TestTheDefaultRunsWhenNothingFires(t *testing.T) {
	m := mustParse(t, complete)

	sel := selectPanel(t, m, review.ContextWorktree, profile(1, 10, review.AvailableCount(0)), "")
	if sel.Panel != "quick" {
		t.Errorf("panel = %q, want quick", sel.Panel)
	}
	if len(sel.Fired) != 0 {
		t.Errorf("fired = %v, want nothing", sel.Fired)
	}
}

// A rule raises the panel. The whole point of the mechanism.
func TestARuleRaisesThePanel(t *testing.T) {
	m := mustParse(t, complete)

	sel := selectPanel(t, m, review.ContextWorktree,
		profile(1, 10, review.AvailableCount(0), review.SignalConcurrency), "")

	if sel.Panel != "deep" {
		t.Errorf("panel = %q, want deep", sel.Panel)
	}
	if len(sel.Fired) != 1 || sel.Fired[0].Index != 1 {
		t.Errorf("fired = %v, want escalate[1]", sel.Fired)
	}
}

// Rules only ever raise. A rule whose target is shallower than the context's
// default fires and changes nothing, so a mistaken rule costs money and never
// produces a review shallower than the repo asked for.
func TestARuleNeverLowersThePanel(t *testing.T) {
	m := mustParse(t, complete)

	// The pr context defaults to standard; the changed_files rule targets
	// standard, and the signals rule targets deep. Against a change that
	// triggers only the shallower one, the default holds.
	sel := selectPanel(t, m, review.ContextPR, profile(40, 900, review.AvailableCount(0)), "")

	if sel.Panel != "standard" {
		t.Errorf("panel = %q, want standard", sel.Panel)
	}
	if len(sel.Fired) != 1 {
		t.Fatalf("fired = %v, want the changed_files rule", sel.Fired)
	}
}

// Every rule is evaluated and the highest target wins, so the order they are
// written in carries no meaning.
func TestOrderCarriesNoMeaning(t *testing.T) {
	m := mustParse(t, complete)
	p := withPaths(profile(30, 900, review.AvailableCount(0)), "internal/auth/token.go")

	sel := selectPanel(t, m, review.ContextWorktree, p, "")

	if sel.Panel != "deep" {
		t.Errorf("panel = %q, want deep — the deepest target among the rules that fired", sel.Panel)
	}
	if len(sel.Fired) != 2 {
		t.Fatalf("fired = %v, want both the touches rule and the changed_files rule", sel.Fired)
	}
	// The rule targeting the shallower panel fired last and did not win, so
	// neither the order they are written in nor the order they fired decides.
	if sel.Fired[1].To != "standard" {
		t.Errorf("fired[1] targets %q, want standard", sel.Fired[1].To)
	}
}

// An override names the panel outright, and the rules are still reported so
// --explain can show what would have happened.
func TestAnOverrideDecidesButStillReports(t *testing.T) {
	m := mustParse(t, complete)

	sel := selectPanel(t, m, review.ContextWorktree,
		profile(1, 10, review.AvailableCount(0), review.SignalConcurrency), "quick")

	if sel.Panel != "quick" {
		t.Errorf("panel = %q, want the override", sel.Panel)
	}
	if !sel.Overridden {
		t.Error("Overridden should record that the rules did not decide")
	}
	if len(sel.Fired) != 1 {
		t.Errorf("fired = %v, want the rule reported even though it did not decide", sel.Fired)
	}
}

func TestAnOverrideNamingNoPanelIsRefused(t *testing.T) {
	m := mustParse(t, complete)

	_, err := review.Select(m, review.ContextWorktree, profile(1, 10, review.AvailableCount(0)), "thorough")
	if err == nil {
		t.Fatal("Select accepted a panel that does not exist")
	}
	if !strings.Contains(err.Error(), "deep, quick, standard") {
		t.Errorf("error = %q, want it to list the declared panels", err)
	}
}

// Unavailable is never low. A rule reading a count the change could not
// produce is refused, rather than evaluating as though the count were zero and
// leaving a repo with an escalation that can never fire.
func TestARuleOverAnUnavailableCountIsRefused(t *testing.T) {
	m := mustParse(t, strings.Replace(complete,
		"      - changed_files: {gte: 20}", "      - referencing_files: {gte: 20}", 1))
	p := profile(1, 10, review.UnavailableCount("no symbol extractor for kotlin"))

	_, err := review.Select(m, review.ContextWorktree, p, "")
	if err == nil {
		t.Fatal("Select evaluated a rule over a count that does not exist")
	}
	for _, want := range []string{"referencing_files", "no symbol extractor for kotlin", "can never fire"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

// The same rule for a signal that could not be determined: a budget that ran
// out is not evidence that nothing was undone.
func TestARuleOverAnUndeterminedSignalIsRefused(t *testing.T) {
	m := mustParse(t, complete)
	set := review.NewSignalSet()
	set.MarkUndetermined(review.SignalFixRevert, "the 200-file blame budget ran out")
	p := &review.Profile{ChangedFiles: 1, ChangedLines: 10, Signals: set, ReferencingFiles: review.AvailableCount(0)}

	_, err := review.Select(m, review.ContextWorktree, p, "")
	if err == nil {
		t.Fatal("Select evaluated a rule over a signal that could not be read")
	}
	if !strings.Contains(err.Error(), "not a signal the change does not carry") {
		t.Errorf("error = %q", err)
	}
}

// A context that posts validates whatever its panel says, because a false
// finding there is published and blocks approval.
func TestAPostingContextAlwaysValidates(t *testing.T) {
	src := strings.Replace(complete,
		"  standard: {reviewers: [correctness, security]}",
		"  standard: {reviewers: [correctness, security], validate: false}", 1)
	m := mustParse(t, src)

	pr := selectPanel(t, m, review.ContextPR, profile(1, 10, review.AvailableCount(0)), "")
	if !pr.Validates {
		t.Error("the pr context must validate even against a panel that says not to")
	}

	worktree := selectPanel(t, m, review.ContextWorktree, profile(1, 10, review.AvailableCount(0)), "")
	if worktree.Validates {
		t.Error("the worktree context follows the panel, which said not to")
	}
}

// --explain names the default, every rule that fired, and what resulted.
func TestExplainShowsTheWholeDecision(t *testing.T) {
	m := mustParse(t, complete)
	p := withPaths(profile(30, 900, review.AvailableCount(4)), "internal/auth/token.go")
	sel := selectPanel(t, m, review.ContextWorktree, p, "")

	out := sel.Explain(m, p)
	for _, want := range []string{
		"default: quick",
		"escalate[0] all → deep",
		"escalate[2] all → standard",
		"panel:   deep",
		"quorum 2",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("explain missing %q:\n%s", want, out)
		}
	}
}

// Equal cost is not a raise. Two panels that spend the same are the same
// depth, and swapping one for the other would change what runs for no reason
// a reader of the manifest could see — here it would trade a reviewer run
// twice for two reviewers run once, losing the corroboration the quorum was
// configured for without gaining depth.
func TestAnEqualCostPanelDoesNotReplaceTheDefault(t *testing.T) {
	src := `
version: 1
reviewers:
  correctness: {provider: claudecode, prompt: builtin:correctness}
  security:    {provider: claudecode, prompt: builtin:security}
judge:     {provider: claudecode, prompt: builtin:judge}
validator: {provider: claudecode, prompt: builtin:validator}
panels:
  doubled: {reviewers: [correctness], quorum: 2}
  paired:  {reviewers: [correctness, security]}
defaults: {worktree: doubled, pr: doubled}
escalate:
  - to: paired
    all:
      - changed_files: {gte: 1}
`
	m := mustParse(t, src)
	if m.Panels["doubled"].Cost() != m.Panels["paired"].Cost() {
		t.Fatalf("the two panels must cost the same for this test to mean anything")
	}

	sel := selectPanel(t, m, review.ContextWorktree, profile(5, 50, review.AvailableCount(0)), "")

	if sel.Panel != "doubled" {
		t.Errorf("panel = %q, want the default to hold against an equal-cost target", sel.Panel)
	}
	if len(sel.Fired) != 1 {
		t.Errorf("the rule should still be reported as fired, having simply raised nothing: %v", sel.Fired)
	}
}

// A rule a repo wrote is a protection it asked for, so a change the rule
// cannot be read against is a refusal it needs to see.
func TestARepoOwnRuleIsRefusedWhenItCannotBeRead(t *testing.T) {
	m := mustParse(t, strings.Replace(complete,
		"      - changed_files: {gte: 20}", "      - referencing_files: {gte: 20}", 1))
	p := profile(1, 10, review.UnavailableCount("no symbol extractor for unrecognised files"))

	_, err := review.Select(m, review.ContextWorktree, p, "")
	if err == nil {
		t.Fatal("a repo's own rule was skipped rather than refused")
	}
	if !strings.Contains(err.Error(), "can never fire") {
		t.Errorf("the refusal should say what to do about it: %v", err)
	}
}

// The built-in default was never asked for. Refusing there turns a language
// the toolkit does not recognise into a review that cannot run at all, in
// exactly the repos with no manifest to edit — and the advice the refusal
// gives cannot be taken, because the rule ships in the binary.
func TestTheBuiltinDefaultSkipsARuleItCannotRead(t *testing.T) {
	m, err := review.DefaultManifest()
	if err != nil {
		t.Fatalf("DefaultManifest: %v", err)
	}
	p := profile(1, 10, review.UnavailableCount("no symbol extractor for unrecognised files"))

	sel, err := review.Select(m, review.ContextWorktree, p, "")
	if err != nil {
		t.Fatalf("the built-in default refused a review it was never asked to gate: %v", err)
	}
	if len(sel.Skipped) != 1 {
		t.Fatalf("skipped = %v, want the referencing_files rule reported", sel.Skipped)
	}
	// Reported, never silent: a rule that quietly never fires is the failure
	// the whole unavailable-is-never-low rule exists to prevent.
	if !strings.Contains(sel.Explain(m, p), "skipped:") {
		t.Errorf("explain does not report the skipped rule:\n%s", sel.Explain(m, p))
	}
}

// Excluded files are grouped by reason and truncated, so a dependency bump
// reports one line rather than four hundred.
func TestExplainSummarisesExclusions(t *testing.T) {
	m := mustParse(t, complete)
	p := profile(1, 10, review.AvailableCount(0))
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		p.Files = append(p.Files, review.ChangedFile{
			DiffFile: review.DiffFile{Path: "vendor/" + name + ".go"},
			Excluded: review.ExcludedVendored,
		})
	}
	p.Files = append(p.Files, review.ChangedFile{
		DiffFile: review.DiffFile{Path: "go.sum"},
		Excluded: review.ExcludedLockfile,
	})

	out := selectPanel(t, m, review.ContextWorktree, p, "").Explain(m, p)

	for _, want := range []string{"excluded (6)", "lockfile: go.sum", "vendored: ", "and 2 more"} {
		if !strings.Contains(out, want) {
			t.Errorf("explain missing %q:\n%s", want, out)
		}
	}
}

// A signal that could not be determined only blocks an answer it could still
// change. `in:` is satisfied by any one member, so a change that demonstrably
// carries concurrency escalates even when fix-revert's budget ran out — which
// is what happens on exactly the large changes the deep panel exists for.
func TestAnUndeterminedSignalDoesNotMaskOneThatIsPresent(t *testing.T) {
	m, err := review.DefaultManifest()
	if err != nil {
		t.Fatalf("DefaultManifest: %v", err)
	}

	set := review.NewSignalSet()
	set.Add(review.SignalConcurrency)
	set.MarkUndetermined(review.SignalFixRevert, "the 200-hunk history budget ran out")
	p := &review.Profile{ChangedFiles: 3, ChangedLines: 40, Signals: set, ReferencingFiles: review.AvailableCount(0)}

	sel, err := review.Select(m, review.ContextPR, p, "")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	// deep-codex rather than deep: the pull-request context escalates on the
	// codex roster, and which roster is not what this test is about.
	if sel.Panel != "deep-codex" {
		t.Errorf("panel = %q, want deep-codex — concurrency is present and the rule is any/in", sel.Panel)
	}
	if len(sel.Skipped) != 0 {
		t.Errorf("skipped = %v, want the rule evaluated rather than abandoned", sel.Skipped)
	}
}

// An `any:` rule fires on any one condition, so one that cannot be read does
// not abandon the rest.
func TestAnAnyRuleSurvivesOneUnreadableCondition(t *testing.T) {
	src := `
version: 1
reviewers:
  correctness: {provider: claudecode, prompt: builtin:correctness}
  security:    {provider: claudecode, prompt: builtin:security}
judge:     {provider: claudecode, prompt: builtin:judge}
validator: {provider: claudecode, prompt: builtin:validator}
panels:
  light: {reviewers: [correctness]}
  heavy: {reviewers: [correctness, security], quorum: 2}
defaults: {worktree: light, pr: light}
escalate:
  - to: heavy
    any:
      - referencing_files: {gte: 20}
      - changed_files: {gte: 1}
`
	m := mustParse(t, src)
	m.Builtin = true
	p := profile(3, 30, review.UnavailableCount("no symbol extractor for unrecognised files"))

	sel, err := review.Select(m, review.ContextWorktree, p, "")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if sel.Panel != "heavy" {
		t.Errorf("panel = %q, want heavy — the second condition holds", sel.Panel)
	}
}

// A panel's shape says what it spends; its description is the only thing that
// says what it is for, which is what a reader deciding whether to override it
// needs.
func TestExplainCarriesThePanelsDescription(t *testing.T) {
	src := `version: 1
reviewers:
  correctness: {provider: claudecode, model: sonnet, prompt: "builtin:correctness"}
judge:     {provider: claudecode, model: opus,   prompt: "builtin:judge"}
validator: {provider: claudecode, model: sonnet, prompt: "builtin:validator"}
panels:
  quick:
    description: the pre-push pass, for a change you already understand
    reviewers: [correctness]
defaults:
  worktree: quick
  pr:       quick
`
	m := mustParse(t, src)
	p := profile(1, 10, review.AvailableCount(0))
	sel := selectPanel(t, m, review.ContextWorktree, p, "")

	out := sel.Explain(m, p)
	if !strings.Contains(out, "the pre-push pass, for a change you already understand") {
		t.Errorf("explain drops the panel's description:\n%s", out)
	}
}

// A manifest that describes none of its panels is listed by name and cost, and
// gains no blank line where a description would have been.
func TestExplainOmitsAnAbsentPanelDescription(t *testing.T) {
	src := `version: 1
reviewers:
  correctness: {provider: claudecode, model: sonnet, prompt: "builtin:correctness"}
judge:     {provider: claudecode, model: opus,   prompt: "builtin:judge"}
validator: {provider: claudecode, model: sonnet, prompt: "builtin:validator"}
panels:
  quick: {reviewers: [correctness]}
defaults:
  worktree: quick
  pr:       quick
`
	m := mustParse(t, src)
	p := profile(1, 10, review.AvailableCount(0))
	sel := selectPanel(t, m, review.ContextWorktree, p, "")

	out := sel.Explain(m, p)
	if strings.Contains(out, "panel:   quick (1 run)\n         \n") {
		t.Errorf("an absent description left a blank line:\n%q", out)
	}
}

// The manifest that ships with agtk describes every panel it declares: it is
// the one a repo with no manifest is reviewed by, and the example every
// consumer copies from.
func TestTheBuiltInPanelsAreAllDescribed(t *testing.T) {
	m, err := review.DefaultManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Panels) == 0 {
		t.Fatal("the built-in manifest declares no panels")
	}
	for name, panel := range m.Panels {
		if strings.TrimSpace(panel.Description) == "" {
			t.Errorf("the built-in panel %q has no description", name)
		}
	}
}
