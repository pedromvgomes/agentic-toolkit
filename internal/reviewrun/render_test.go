package reviewrun

import (
	"bytes"
	"strings"
	"testing"
)

func render(r *Review) string {
	var b bytes.Buffer
	Render(&b, r)
	return b.String()
}

// Numbered continuously across severities, so somebody can say "fix 1, 4 and
// 7" without saying which table they meant.
func TestFindingsAreNumberedContinuouslyAcrossSeverities(t *testing.T) {
	out := render(&Review{
		Available: true, Panel: "deep", Manifest: "m", Range: "main...HEAD",
		Findings: []Finding{
			{Severity: SeverityRed, Path: "a.go", StartLine: ptr(1), Category: "correctness", Issue: "one"},
			{Severity: SeverityAmber, Path: "b.go", StartLine: ptr(2), Category: "security", Issue: "two"},
			{Severity: SeverityGreen, Path: "c.go", StartLine: ptr(3), Category: "perf", Issue: "three"},
		},
	})
	for _, want := range []string{" 1. a.go:1", " 2. b.go:2", " 3. c.go:3"} {
		if !strings.Contains(out, want) {
			t.Errorf("the numbering omits %q:\n%s", want, out)
		}
	}
	for _, want := range []string{"RED", "AMBER", "GREEN", "must fix before merge"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output omits %q:\n%s", want, out)
		}
	}
}

func TestAnEmptySeverityGetsNoTable(t *testing.T) {
	out := render(&Review{
		Available: true, Panel: "quick",
		Findings: []Finding{{Severity: SeverityRed, Path: "a.go", StartLine: ptr(1), Issue: "one"}},
	})
	if strings.Contains(out, "GREEN") {
		t.Errorf("an empty severity was given a heading:\n%s", out)
	}
}

func TestACleanReviewSaysSoRatherThanPrintingNothing(t *testing.T) {
	out := render(&Review{Available: true, Panel: "quick"})
	if !strings.Contains(out, "No findings survived") {
		t.Errorf("a clean review printed no verdict:\n%s", out)
	}
}

// A reviewer that ran and found nothing gets a line. Absent from the tables it
// is indistinguishable from one that never ran, and they mean opposite things.
func TestAReviewerThatFoundNothingIsNamedRatherThanAbsent(t *testing.T) {
	out := render(&Review{
		Available: true, Panel: "standard",
		Reports: []RunReport{
			{Label: "security", Role: RoleReviewer, Report: Answered(nil)},
			{Label: "correctness", Role: RoleReviewer, Report: Answered([]Finding{{Path: "a.go"}})},
		},
	})
	if !strings.Contains(out, "Ran and reported nothing: security") {
		t.Errorf("the silent reviewer is not named:\n%s", out)
	}
	if strings.Contains(out, "nothing: security, correctness") {
		t.Errorf("a reviewer that found something was called silent:\n%s", out)
	}
}

func TestARunThatCouldNotAnswerIsNamedWithItsReason(t *testing.T) {
	out := render(&Review{
		Available: true, Panel: "standard",
		Reports: []RunReport{{Label: "security", Role: RoleReviewer, Report: Unavailable("the CLI timed out")}},
	})
	for _, want := range []string{"Could not answer (1)", "security", "the CLI timed out"} {
		if !strings.Contains(out, want) {
			t.Errorf("the failed run is not reported: %q missing from:\n%s", want, out)
		}
	}
}

// A review with no verdict must not print an empty findings table, which would
// read as a clean review.
func TestAnUnavailableReviewSaysWhyAndPrintsNoTables(t *testing.T) {
	out := render(&Review{
		Panel: "deep", Reason: "the judge could not be run",
		Reports: []RunReport{{Label: "judge", Role: RoleJudge, Report: Unavailable("the judge could not be run")}},
	})
	if !strings.Contains(out, "could not reach a verdict") {
		t.Errorf("an unavailable review does not say so:\n%s", out)
	}
	if strings.Contains(out, "No findings survived") {
		t.Errorf("an unavailable review reads as a clean one:\n%s", out)
	}
}

func TestTheDiscardedJudgeIDsAreReported(t *testing.T) {
	out := render(&Review{Available: true, Panel: "quick", DiscardedIDs: []string{"f99"}})
	if !strings.Contains(out, "f99") || !strings.Contains(out, "did not issue") {
		t.Errorf("the discarded ids are not reported:\n%s", out)
	}
}

func TestWhatIsAbsentFromTheReviewedCopyIsReported(t *testing.T) {
	out := render(&Review{
		Available: true, Panel: "quick",
		Skipped: []Skipped{{"AGENTS.md", SkipInstructionFile}},
	})
	if !strings.Contains(out, "Absent from the reviewed copy") || !strings.Contains(out, "AGENTS.md") {
		t.Errorf("the filtered paths are not reported:\n%s", out)
	}
}

func TestTheGoodListIsRenderedWhenTheJudgeGivesOne(t *testing.T) {
	out := render(&Review{Available: true, Panel: "quick", Good: []string{"clear naming"}})
	if !strings.Contains(out, "What's good") || !strings.Contains(out, "clear naming") {
		t.Errorf("the what's-good list is missing:\n%s", out)
	}
}

func TestAFindingRendersItsCorroborationAndVerdict(t *testing.T) {
	out := render(&Review{
		Available: true, Panel: "deep",
		Findings: []Finding{{
			Severity: SeverityRed, Path: "a.go", StartLine: ptr(1), Category: "c",
			Issue: "boom", Suggestion: "do not", Reviewer: "security",
			Corroboration: 2, Verdict: &Verdict{Verdict: VerdictUpheld},
		}},
	})
	for _, want := range []string{"security", "2 instances agreed", "validator upheld", "fix: do not"} {
		if !strings.Contains(out, want) {
			t.Errorf("the finding omits %q:\n%s", want, out)
		}
	}
}

func TestLocationRendersEveryShape(t *testing.T) {
	for _, tc := range []struct {
		f    Finding
		want string
	}{
		{Finding{}, "(no file)"},
		{Finding{Path: "a.go"}, "a.go"},
		{Finding{Path: "a.go", StartLine: ptr(4)}, "a.go:4"},
		{Finding{Path: "a.go", StartLine: ptr(4), EndLine: ptr(9)}, "a.go:4-9"},
	} {
		if got := location(tc.f); got != tc.want {
			t.Errorf("location(%+v) = %q, want %q", tc.f, got, tc.want)
		}
	}
}

func TestSeverityGlossCoversTheLadder(t *testing.T) {
	for _, s := range Severities {
		if severityGloss(s) == "" {
			t.Errorf("%s has no gloss", s)
		}
	}
	if severityGloss(Severity("CRITICAL")) != "" {
		t.Error("an invented severity was given a gloss")
	}
}

// A preview that withheld the prompts would not be a preview of what gets
// sent.
func TestThePlanPrintsThePromptsAndTheRuns(t *testing.T) {
	var b bytes.Buffer
	RenderPlan(&b, &Plan{
		Panel: "quick", Manifest: "m", Range: "main...HEAD",
		Material: Material{
			Root:        &Root{Code: "/tmp/x/root", Work: "/tmp/x/work"},
			Conventions: []ConventionDoc{{Path: "CLAUDE.md"}},
		},
		Runs: []PlannedRun{{
			Label: "correctness", Role: RoleReviewer, Provider: "claudecode",
			Model: "sonnet", Prompt: "THE ASSEMBLED PROMPT",
		}},
	})
	out := b.String()
	for _, want := range []string{"quick", "/tmp/x/root", "/tmp/x/work", "CLAUDE.md",
		"1 run(s) would be made", "nothing was spent", "correctness", "claudecode",
		"sonnet", "THE ASSEMBLED PROMPT"} {
		if !strings.Contains(out, want) {
			t.Errorf("the plan omits %q:\n%s", want, out)
		}
	}
}

func TestThePlanSaysWhenNoConventionDocsWereFound(t *testing.T) {
	var b bytes.Buffer
	RenderPlan(&b, &Plan{
		Panel:    "quick",
		Material: Material{Root: &Root{Code: "/r", Work: "/w"}},
	})
	if !strings.Contains(b.String(), "none found") {
		t.Errorf("an empty conventions list is not reported:\n%s", b.String())
	}
}

// A runner that names no model leaves the CLI's own default, and the preview
// has to say that rather than print an empty column.
func TestThePlanNamesTheDefaultModelExplicitly(t *testing.T) {
	var b bytes.Buffer
	RenderPlan(&b, &Plan{
		Panel:    "quick",
		Material: Material{Root: &Root{Code: "/r", Work: "/w"}},
		Runs:     []PlannedRun{{Label: "x", Role: RoleReviewer, Provider: "claudecode"}},
	})
	if !strings.Contains(b.String(), "the CLI's own default") {
		t.Errorf("an unset model renders as blank:\n%s", b.String())
	}
}
