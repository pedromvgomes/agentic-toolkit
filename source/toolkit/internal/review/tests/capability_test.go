package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/review"
)

func manifestWithProvider(name string) string {
	return `
version: 1
reviewers:
  correctness: {provider: ` + name + `, prompt: builtin:correctness}
judge:     {provider: ` + name + `, prompt: builtin:judge}
validator: {provider: ` + name + `, prompt: builtin:validator}
panels:
  quick: {reviewers: [correctness]}
defaults: {worktree: quick, pr: quick}
`
}

// Both shipped providers can express what a review run needs. Codex has no
// per-tool allowlist, so it is confined by a sandbox mode instead — which the
// provider table works out by asking rather than by knowing which CLI it has.
func TestTheShippedProvidersCanStaffAReview(t *testing.T) {
	for _, name := range []string{"claudecode", "codex"} {
		t.Run(name, func(t *testing.T) {
			m := mustParse(t, manifestWithProvider(name))
			if err := review.CheckCapabilities("manifest.yaml", m); err != nil {
				t.Errorf("CheckCapabilities: %v", err)
			}
		})
	}
}

// A provider nobody has written is a gap in the driver, and the refusal
// arrives from the manifest rather than from a process that failed to start.
func TestAnUnknownProviderIsRefusedFromTheManifest(t *testing.T) {
	m := mustParse(t, manifestWithProvider("gemini"))

	err := review.CheckCapabilities("manifest.yaml", m)
	if err == nil {
		t.Fatal("CheckCapabilities accepted a provider that does not exist")
	}
	for _, want := range []string{"reviewers.correctness.provider", `"gemini" is not a provider`, "claudecode, codex"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}
