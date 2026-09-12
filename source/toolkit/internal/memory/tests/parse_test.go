package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

// TestParseNote reads the frontmatter and keeps the body intact.
func TestParseNote(t *testing.T) {
	raw := note("lockfile-pins-shas-not-tags", "  - path: internal/resolver/graph.go\n    blob: 3cda36759c36\n")

	n, err := memory.ParseNote("notes/lockfile-pins-shas-not-tags.md", []byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if n.Name != "lockfile-pins-shas-not-tags" || n.Kind != memory.KindInvariant || n.Confidence != memory.ConfidenceVerified {
		t.Errorf("frontmatter decoded wrong: %+v", n)
	}
	if len(n.Anchors) != 1 || n.Anchors[0].Blob != "3cda36759c36" {
		t.Errorf("anchors decoded wrong: %+v", n.Anchors)
	}
	if !strings.Contains(n.Body, "graph.go:88") {
		t.Errorf("body lost: %q", n.Body)
	}
}

// TestParseNoteRejectsUnknownField: a typo'd key must not decode into a
// note that merely looks anchored — such a note would never be checked.
func TestParseNoteRejectsUnknownField(t *testing.T) {
	raw := "---\nname: n\nkind: gotcha\ndescription: d\nanchor: internal/x.go\nconfidence: suspect\n---\n\nbody\n"

	if _, err := memory.ParseNote("n.md", []byte(raw)); err == nil {
		t.Fatal("expected an error for unknown frontmatter field `anchor`")
	}
}

// TestParseNoteRequiresFrontmatter covers both delimiter failures.
func TestParseNoteRequiresFrontmatter(t *testing.T) {
	for name, raw := range map[string]string{
		"no frontmatter": "just a body\n",
		"unclosed":       "---\nname: n\n\nbody\n",
	} {
		if _, err := memory.ParseNote("n.md", []byte(raw)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// TestRenderRoundTrip: rendering a parsed note reproduces it byte-for-byte,
// so `anchor` never rewrites a note it did not actually change.
func TestRenderRoundTrip(t *testing.T) {
	raw := note("lockfile-pins-shas-not-tags", "  - path: internal/resolver/graph.go\n    blob: 3cda36759c36\n")

	n, err := memory.ParseNote("n.md", []byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := n.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if string(out) != raw {
		t.Errorf("round trip changed the file:\n--- got ---\n%s\n--- want ---\n%s", out, raw)
	}
}
