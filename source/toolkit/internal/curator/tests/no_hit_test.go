package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/curator"
)

// A hit records that a reader found a note useful. The curator's own reads are
// upkeep, so the prompt tells it to pass --no-hit on every `memory show`, and
// the grant it runs under admits that longer form.
func TestThePromptTellsTheCuratorToReadNotesWithoutRecordingAHit(t *testing.T) {
	fake, _, err := run(t, curatedEnvelope, curator.Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	prompt := fake.Stdin(t)
	if !strings.Contains(prompt, "agtk memory show <name> --no-hit") {
		t.Errorf("the prompt does not tell the curator to read a note with --no-hit: %q", prompt)
	}
	for _, line := range strings.Split(prompt, "\n") {
		if strings.Contains(line, "agtk memory show") && !strings.Contains(line, "--no-hit") {
			t.Errorf("the prompt has a note read that would record a hit: %q", line)
		}
	}

	argv := strings.Join(fake.Recorded(t).Args, "\x00")
	if !strings.Contains(argv, "Bash(agtk memory show *)") {
		t.Errorf("the grant does not admit `agtk memory show <name> --no-hit`: %q", argv)
	}
}
