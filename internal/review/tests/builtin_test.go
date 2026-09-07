package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

// The default is held to the same validation every consumer's manifest is. A
// default that could not itself pass would be a rule the toolkit does not
// follow.
func TestTheEmbeddedDefaultParses(t *testing.T) {
	m, err := review.DefaultManifest()
	if err != nil {
		t.Fatalf("DefaultManifest: %v", err)
	}
	if err := review.CheckCapabilities("<built-in default>", m); err != nil {
		t.Errorf("CheckCapabilities: %v", err)
	}
}

// The panels a default escalates between have to be ordered by what they
// spend, or "rules only raise" is a sentence rather than a property.
func TestTheDefaultPanelsAreOrderedByCost(t *testing.T) {
	m, err := review.DefaultManifest()
	if err != nil {
		t.Fatalf("DefaultManifest: %v", err)
	}
	quick, standard, deep := m.Panels["quick"].Cost(), m.Panels["standard"].Cost(), m.Panels["deep"].Cost()
	if !(quick < standard && standard < deep) {
		t.Errorf("costs quick=%d standard=%d deep=%d, want strictly increasing", quick, standard, deep)
	}
}

// A misspelled builtin prompt is a reviewer that would start with no
// instructions and answer anyway, so it is refused when the manifest is read.
func TestAMisspelledBuiltinPromptIsRefused(t *testing.T) {
	err := refuse(t, strings.Replace(complete, "builtin:correctness", "builtin:corectness", 1))

	if !review.IsKind(err, review.ErrInvalidPrompt) {
		t.Fatalf("kind = %v, want invalid_prompt", err)
	}
	for _, want := range []string{"corectness", "unified, correctness, security"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

// A repo-local prompt is resolved against the manifest's own directory, so a
// manifest and the prompts it names travel as one tree — which is what lets
// agtk read both from the base ref and a branch rewrite neither.
func TestARepoPromptStaysInsideTheManifestDirectory(t *testing.T) {
	for _, bad := range []string{"./../../etc/passwd", "./prompts/../../../x.md"} {
		t.Run(bad, func(t *testing.T) {
			err := refuse(t, strings.Replace(complete, "./prompts/perf.md", bad, 1))
			if !strings.Contains(err.Error(), ".agents/code-review") {
				t.Errorf("error = %q, want it to name the directory the path must stay inside", err)
			}
		})
	}
}

// Two forms and no third: a bare name would have to be resolved against
// something, and the lookup order would silently decide which body a reviewer
// got.
func TestABarePromptNameIsRefused(t *testing.T) {
	err := refuse(t, strings.Replace(complete, "builtin:correctness", "correctness", 1))

	if !strings.Contains(err.Error(), "neither `builtin:<name>` nor a path starting with `./`") {
		t.Errorf("error = %q", err)
	}
}
