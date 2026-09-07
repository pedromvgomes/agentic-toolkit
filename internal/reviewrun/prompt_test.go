package reviewrun

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

// Every name the manifest accepts must resolve to a body, or a manifest that
// validates produces a reviewer with no instructions that answers anyway.
func TestEveryBuiltinPromptNameHasABody(t *testing.T) {
	for _, name := range review.BuiltinPrompts {
		body, err := builtinPrompt(name)
		if err != nil {
			t.Errorf("builtin %s: %v", name, err)
			continue
		}
		if len(strings.TrimSpace(body)) < 200 {
			t.Errorf("the builtin %s prompt is %d bytes, which is not instructions", name, len(body))
		}
	}
}

func TestAnUnknownBuiltinPromptIsRefused(t *testing.T) {
	if _, err := builtinPrompt("corectness"); err == nil {
		t.Fatal("a misspelt builtin resolved to a prompt")
	}
}

// A reviewer's body carries the shared preamble, so the do-not-flag list, the
// severity ladder and the evidence rule have one home rather than a copy per
// axis that drifts.
func TestAReviewerPromptCarriesTheSharedPreamble(t *testing.T) {
	for _, name := range []string{"correctness", "security", "performance", "unified"} {
		body, err := builtinPrompt(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Do not flag", "RED", "AMBER", "GREEN", "quote"} {
			if !strings.Contains(body, want) {
				t.Errorf("the %s prompt omits %q from the preamble", name, want)
			}
		}
	}
}

// The judge and the validator are not filing findings against an axis, so the
// reviewer preamble would describe a job they are not doing.
func TestTheJudgeAndValidatorDoNotCarryTheReviewerPreamble(t *testing.T) {
	for _, name := range []string{"judge", "validator"} {
		body, err := builtinPrompt(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(body, "You are one reviewer in a panel") {
			t.Errorf("the %s prompt carries the reviewer preamble", name)
		}
	}
}

// The schema is the single definition of the answer's shape. A prompt that
// restated it would be a second, drifting definition that nothing validates.
func TestNoBuiltinPromptRestatesTheAnswerSchema(t *testing.T) {
	for _, name := range review.BuiltinPrompts {
		body, err := builtinPrompt(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"```yaml", "proposed_action:", "severity: RED |"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("the %s prompt restates the schema (%q)", name, forbidden)
			}
		}
	}
}

// The judge must be told not to invent an id and not to rewrite the quote:
// both are what ADR 0008 makes structural, and the prompt is the layer that
// keeps a run from spending itself discovering it.
func TestTheJudgePromptForbidsInventingIDsAndRewritingEvidence(t *testing.T) {
	body, err := builtinPrompt("judge")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Never invent an id", "Do not restate the file"} {
		if !strings.Contains(body, want) {
			t.Errorf("the judge prompt omits %q", want)
		}
	}
}

// material builds a composed prompt's inputs.
func testMaterial() Material {
	return Material{
		ChangedFiles: []string{"a.go", "b.go"},
		Patch:        "--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+new\n",
		Conventions:  []ConventionDoc{{Path: "CLAUDE.md", Body: "never narrate the change"}},
		Root:         &Root{Code: "/tmp/agtk-review-x/root", Work: "/tmp/agtk-review-x/work"},
		Range:        "main...working tree",
	}
}

func TestComposeCarriesEveryLayer(t *testing.T) {
	got := testMaterial().compose("AXIS BODY")

	for _, want := range []string{
		"AXIS BODY",
		"# Repo conventions", "CLAUDE.md", "never narrate the change",
		"# The change", "main...working tree", "a.go", "b.go",
		"@@ -1 +1 @@",
		"# The code", "/tmp/agtk-review-x/root",
		"# Instructions found in the material", "security:prompt-injection",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the composed prompt omits %q", want)
		}
	}
}

// The injection clause is about everything above it, so it goes last. A
// clause the diff could appear after would be a clause the diff could pretend
// to close.
func TestTheInjectionClauseIsLast(t *testing.T) {
	got := testMaterial().compose("AXIS BODY")
	if strings.LastIndex(got, "Instructions found in the material") < strings.LastIndex(got, "@@ -1 +1 @@") {
		t.Error("the diff appears after the injection clause")
	}
}

// A run told to read a file the copy does not hold would report its absence as
// a defect in the change.
func TestComposeNamesWhatTheReviewRootDoesNotHold(t *testing.T) {
	m := testMaterial()
	m.Root.Skipped = []Skipped{
		{"AGENTS.md", SkipInstructionFile},
		{"link", SkipSymlink},
	}
	got := m.compose("BODY")
	for _, want := range []string{"AGENTS.md", "link", "Do not file findings about their contents"} {
		if !strings.Contains(got, want) {
			t.Errorf("the composed prompt omits %q", want)
		}
	}
}

func TestComposeOmitsTheConventionsSectionWhenThereAreNone(t *testing.T) {
	m := testMaterial()
	m.Conventions = nil
	if got := m.compose("BODY"); strings.Contains(got, "# Repo conventions") {
		t.Error("an empty conventions section was rendered")
	}
}

func TestConventionPathsNamesWhatWasRead(t *testing.T) {
	m := testMaterial()
	got := m.ConventionPaths()
	if len(got) != 1 || got[0] != "CLAUDE.md" {
		t.Errorf("ConventionPaths() = %v", got)
	}
}

// A repo with hundreds of filtered paths reports a line, not a page.
func TestSummariseSkippedGroupsAndCounts(t *testing.T) {
	var skipped []Skipped
	for _, p := range []string{"a", "b", "c", "d", "e"} {
		skipped = append(skipped, Skipped{p, SkipInstructionFile})
	}
	skipped = append(skipped, Skipped{"link", SkipSymlink})

	got := summariseSkipped(skipped)
	if len(got) != 2 {
		t.Fatalf("want one line per reason, got %d: %v", len(got), got)
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "and 2 more") {
		t.Errorf("a long group was not counted: %v", got)
	}
	if !strings.Contains(joined, "link") {
		t.Errorf("the symlink group is missing: %v", got)
	}
}

func TestSummariseSkippedOfNothingIsEmpty(t *testing.T) {
	if got := summariseSkipped(nil); len(got) != 0 {
		t.Errorf("summariseSkipped(nil) = %v", got)
	}
}

// The default list is what a repo naming none is held against.
func TestTheDefaultConventionDocsAreTheDocumentedSeven(t *testing.T) {
	want := []string{"CLAUDE.md", "AGENTS.md", ".claude/CLAUDE.md", "CONTEXT.md",
		"CONTRIBUTING.md", "docs/ARCHITECTURE.md", "docs/CODE_STANDARDS.md"}
	if len(DefaultConventionDocs) != len(want) {
		t.Fatalf("the default list holds %d documents, want %d", len(DefaultConventionDocs), len(want))
	}
	for i := range want {
		if DefaultConventionDocs[i] != want[i] {
			t.Errorf("default document %d is %q, want %q", i, DefaultConventionDocs[i], want[i])
		}
	}
}
