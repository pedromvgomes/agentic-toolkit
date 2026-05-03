package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
)

func TestRenderTopLevelError_ParseErrorMultiLine(t *testing.T) {
	pe := &stack.ParseError{
		Path:    "/tmp/.agentic-toolkit.yaml",
		Kind:    stack.ErrInvalidEntry,
		Message: "skills[0]: skill name \"foo/bar\" cannot contain '/'; only commands support nested names like 'group/cmd'",
	}
	var buf bytes.Buffer
	renderTopLevelError(&buf, pe)
	got := buf.String()

	wantLines := []string{
		"agtk: failed to parse stack manifest",
		"  file:    /tmp/.agentic-toolkit.yaml",
		"  reason:  invalid_entry",
		"  detail:  skills[0]: skill name \"foo/bar\" cannot contain '/'; only commands support nested names like 'group/cmd'",
	}
	for _, line := range wantLines {
		if !strings.Contains(got, line) {
			t.Errorf("output missing line %q\nfull:\n%s", line, got)
		}
	}
	// Path must appear exactly once — the original bug duplicated it.
	if c := strings.Count(got, "/tmp/.agentic-toolkit.yaml"); c != 1 {
		t.Errorf("path appeared %d times, want 1\nfull:\n%s", c, got)
	}
}

func TestRenderTopLevelError_ParseErrorWithLineCol(t *testing.T) {
	pe := &stack.ParseError{
		Path:    "x.yaml",
		Line:    3,
		Column:  5,
		Kind:    stack.ErrYAMLSyntax,
		Message: "unexpected token",
	}
	var buf bytes.Buffer
	renderTopLevelError(&buf, pe)
	got := buf.String()
	if !strings.Contains(got, "  file:    x.yaml:3:5") {
		t.Errorf("expected line:col in file row, got:\n%s", got)
	}
}

func TestRenderTopLevelError_PlainError(t *testing.T) {
	var buf bytes.Buffer
	renderTopLevelError(&buf, errors.New("boom"))
	got := buf.String()
	if got != "agtk: boom\n" {
		t.Errorf("plain error render = %q, want %q", got, "agtk: boom\n")
	}
}
