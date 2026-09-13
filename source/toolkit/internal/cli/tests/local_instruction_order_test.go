package tests

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRender_LocalInstructions_FilenameOrderWinsOverDeclaredName: the
// concatenated instruction file lists local.context first and then the
// local.instructions in filename order — the knob the consumer has —
// rather than the alphabetical order of the `name:` they declare.
func TestRender_LocalInstructions_FilenameOrderWinsOverDeclaredName(t *testing.T) {
	work := t.TempDir()

	writeFile(t, filepath.Join(work, ".agentic-toolkit.yaml"),
		"platforms:\n  - claude\n  - codex\n"+
			"local:\n  context: local/CONTEXT.md\n  instructions: local/instructions\n")
	writeFile(t, filepath.Join(work, "local/CONTEXT.md"), "CONTEXT BODY\n")
	writeFile(t, filepath.Join(work, "local/instructions/00-first.md"),
		"---\nname: zzz-second\ndescription: first by filename\n---\n\nFIRST BODY\n")
	writeFile(t, filepath.Join(work, "local/instructions/10-second.md"),
		"---\nname: aaa-first\ndescription: second by filename\n---\n\nSECOND BODY\n")

	if _, _, err := runCLI(t, work, "lock"); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if _, _, err := runCLI(t, work, "render"); err != nil {
		t.Fatalf("render: %v", err)
	}

	for _, rel := range []string{"CLAUDE.md", "AGENTS.md"} {
		got := readFile(t, filepath.Join(work, rel))
		context := strings.Index(got, "CONTEXT BODY")
		first := strings.Index(got, "FIRST BODY")
		second := strings.Index(got, "SECOND BODY")
		if context < 0 || first < 0 || second < 0 {
			t.Fatalf("%s missing a body: %q", rel, got)
		}
		if context > first {
			t.Errorf("%s: local.context must render before local.instructions, got %q", rel, got)
		}
		if first > second {
			t.Errorf("%s: 00-first.md must render before 10-second.md, got %q", rel, got)
		}
	}
}
