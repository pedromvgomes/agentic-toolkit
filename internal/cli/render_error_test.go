package cli

import (
	"bytes"
	"errors"
	"fmt"
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

func TestRenderTopLevelError_WrappedChain(t *testing.T) {
	// Most common shape: cli wraps resolver wraps provider wraps
	// runGit. flattenError should walk the %w chain and produce one
	// labelled row per layer instead of one ":"-joined wall of text.
	leaf := errors.New("repository not found")
	mid := fmt.Errorf("ls-remote https://x.example/repo: %w", leaf)
	top := fmt.Errorf("resolve: %w", mid)

	var buf bytes.Buffer
	renderTopLevelError(&buf, top)
	got := buf.String()

	for _, want := range []string{
		"agtk: command failed",
		"  → resolve",
		"  → ls-remote https://x.example/repo",
		"  → repository not found",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull:\n%s", want, got)
		}
	}
}

func TestRenderTopLevelError_JoinedSiblings(t *testing.T) {
	// errors.Join produces siblings, not nested wrappers. flattenError
	// should walk both and emit a "— and —" separator so the user can
	// tell they're parallel failures rather than a single chain.
	a := fmt.Errorf("stack A: %w", errors.New("missing repo"))
	b := fmt.Errorf("stack B: %w", errors.New("auth required"))
	joined := errors.Join(a, b)

	var buf bytes.Buffer
	renderTopLevelError(&buf, joined)
	got := buf.String()

	for _, want := range []string{"stack A", "missing repo", "— and —", "stack B", "auth required"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull:\n%s", want, got)
		}
	}
}
