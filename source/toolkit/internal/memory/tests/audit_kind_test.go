package tests

import (
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

// TestAuditDistinguishesUnstampedFromMissing: an agent triages on Kind, and
// acting on "missing" means dropping the anchor — wrong for a file that is
// sitting right there and merely needs stamping.
func TestAuditDistinguishesUnstampedFromMissing(t *testing.T) {
	s := project(t, map[string]string{"internal/resolver/graph.go": "package resolver\n"})
	writeNote(t, s, "fresh", note("fresh", "  - path: internal/resolver/graph.go\n"))

	drifts := s.AuditNote(loadOne(t, s, "fresh")).Drifts
	if len(drifts) != 1 {
		t.Fatalf("drifts = %+v, want one", drifts)
	}
	if drifts[0].Kind != memory.DriftUnstamped {
		t.Errorf("kind = %q, want %q for a present but unstamped anchor", drifts[0].Kind, memory.DriftUnstamped)
	}
	if drifts[0].Now == "" {
		t.Error("the current hash should be reported so the reader can see what would be stamped")
	}
}
