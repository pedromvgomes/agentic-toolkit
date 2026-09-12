package tests

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/codex"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// TestCommand_ConvertedToSkill: Codex has no command construct, so a
// command renders as a skill carrying the instruction that stands in for
// Claude's disable-model-invocation — the restriction is in the body
// because Codex's frontmatter has nowhere to put it.
func TestCommand_ConvertedToSkill(t *testing.T) {
	tmp := t.TempDir()

	plan := makePlan([]resolver.PlannedDefinition{
		pdCommand("deploy", "ship it", "deploy body\n", "<env>", "default"),
	}, "default")
	renderCodex(t, plan, tmp)

	got := mustRead(t, filepath.Join(tmp, ".agents/skills/deploy/SKILL.md"))
	for _, want := range []string{
		"name: deploy",
		"description: ship it",
		"do not invoke it on your own",
		"Arguments: <env>",
		"deploy body",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("converted skill missing %q:\n%s", want, got)
		}
	}

	manifest := mustReadJSON(t, filepath.Join(tmp, ".agents/.agtk-manifest.json"))
	files, _ := manifest["files"].(map[string]any)
	if _, ok := files[".agents/skills/deploy/SKILL.md"]; !ok {
		t.Errorf("converted skill untracked: %v", mapKeys(files))
	}
}

// TestCommand_SkillWinsTheNameCollision: both categories resolve onto
// .agents/skills/<name>/SKILL.md. The authored skill wins and the
// command is dropped — the same resolution Claude Code applies to the
// same collision — and it is reported rather than raised, since the
// command still renders as a command on Claude.
func TestCommand_SkillWinsTheNameCollision(t *testing.T) {
	tmp := t.TempDir()
	var out bytes.Buffer

	skillFS := makeFS(map[string]string{"definitions/skills/review/SKILL.md": "ignored\n"})
	plan := makePlan([]resolver.PlannedDefinition{
		pdSkill("review", "the authored skill", "skill body\n", "definitions/skills/review", "default", skillFS),
		pdCommand("review", "the command", "command body\n", "", "default"),
	}, "default")

	if err := codex.Render(plan, codex.Options{
		Scope: codex.ScopeProject, ProjectRoot: tmp, Stdout: &out,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	got := mustRead(t, filepath.Join(tmp, ".agents/skills/review/SKILL.md"))
	if !strings.Contains(got, "skill body") {
		t.Errorf("the authored skill did not win:\n%s", got)
	}
	if strings.Contains(got, "command body") {
		t.Errorf("the command overwrote the skill:\n%s", got)
	}
	if !strings.Contains(out.String(), `command "review" not rendered`) {
		t.Errorf("dropped command not reported: %q", out.String())
	}
}

// TestCommand_SiblingCommandsStayOutOfEachOthersBundles: a command is a
// single file, not a bundle directory, so converting one carries no
// companion files across from wherever it was authored.
func TestCommand_SiblingCommandsStayOutOfEachOthersBundles(t *testing.T) {
	tmp := t.TempDir()

	plan := makePlan([]resolver.PlannedDefinition{
		pdCommand("deploy", "ship it", "deploy body\n", "", "default"),
		pdCommand("rollback", "undo it", "rollback body\n", "", "default"),
	}, "default")
	renderCodex(t, plan, tmp)

	for _, name := range []string{"deploy", "rollback"} {
		entries, err := os.ReadDir(filepath.Join(tmp, ".agents/skills", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if len(entries) != 1 || entries[0].Name() != "SKILL.md" {
			t.Errorf("%s bundle holds more than its own SKILL.md: %v", name, entries)
		}
	}
}
