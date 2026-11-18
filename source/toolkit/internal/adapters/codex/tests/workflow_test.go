package tests

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/codex"
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// everyRenderingCategory is one definition of each category that writes
// something under .agents/ or .codex/, or into AGENTS.md.
func everyRenderingCategory() []resolver.PlannedDefinition {
	skillFS := makeFS(map[string]string{
		"definitions/skills/foo/SKILL.md":         "ignored\n",
		"definitions/skills/foo/prompts/extra.md": "extra prompt\n",
	})
	return []resolver.PlannedDefinition{
		pdSkill("foo", "skill desc", "skill body\n", "definitions/skills/foo", "default", skillFS),
		pdRule("style", "style rule", "style body\n", "default"),
		pdAgent("bar", "agent desc", "agent body\n", "sonnet", "default", nil),
		pdCommand("deploy", "deploy cmd", "deploy body\n", "", "default"),
		pdInstruction("plan-approval", "approve plans", "approve plans body", "default"),
		pdMCP("ctx", definitions.TransportStdio, &definitions.MCPServer{Command: "npx"}, nil, "default"),
		pdHook("guard", "PreToolUse", "Bash", definitions.HookHandler{
			Type: definitions.HandlerCommand, Command: "./guard.sh",
		}, 0, nil, "default"),
		pdSetting("base", map[string]any{"model": "gpt-5"}, "default"),
	}
}

func withWorkflow(defs []resolver.PlannedDefinition) []resolver.PlannedDefinition {
	return append(append([]resolver.PlannedDefinition{}, defs...), pdWorkflow("triage-issue", "Triage an issue", "default"))
}

func projectOpts(tmp string) codex.Options {
	return codex.Options{Scope: codex.ScopeProject, ProjectRoot: tmp}
}

// snapshotTree maps every regular file under root, by slash-separated
// path relative to root, to its content.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

func assertSameTree(t *testing.T, want, got map[string]string) {
	t.Helper()
	for p, body := range got {
		w, ok := want[p]
		if !ok {
			t.Errorf("unexpected file %s", p)
			continue
		}
		if w != body {
			t.Errorf("%s differs:\nwant %q\ngot  %q", p, w, body)
		}
	}
	for p := range want {
		if _, ok := got[p]; !ok {
			t.Errorf("missing file %s", p)
		}
	}
}

// A Workflow Definition in the resolved set adds no file, config.toml key,
// AGENTS.md entry or manifest entry, and every other category renders
// exactly as it does without it.
func TestRender_WorkflowDefinitionWritesNothing(t *testing.T) {
	without := t.TempDir()
	with := t.TempDir()

	if err := codex.Render(makePlan(everyRenderingCategory(), "default"), projectOpts(without)); err != nil {
		t.Fatalf("Render without workflow: %v", err)
	}
	if err := codex.Render(makePlan(withWorkflow(everyRenderingCategory()), "default"), projectOpts(with)); err != nil {
		t.Fatalf("Render with workflow: %v", err)
	}

	assertSameTree(t, snapshotTree(t, without), snapshotTree(t, with))
}

// A resolved set holding only a Workflow Definition renders the same tree
// as an empty one: no AGENTS.md and no config.toml.
func TestRender_WorkflowDefinitionAloneRendersLikeAnEmptyPlan(t *testing.T) {
	empty := t.TempDir()
	only := t.TempDir()

	if err := codex.Render(makePlan(nil, "default"), projectOpts(empty)); err != nil {
		t.Fatalf("Render empty: %v", err)
	}
	if err := codex.Render(makePlan(withWorkflow(nil), "default"), projectOpts(only)); err != nil {
		t.Fatalf("Render workflow only: %v", err)
	}

	assertSameTree(t, snapshotTree(t, empty), snapshotTree(t, only))
	for _, p := range []string{"AGENTS.md", ".codex/config.toml"} {
		if _, err := os.Stat(filepath.Join(only, p)); !os.IsNotExist(err) {
			t.Errorf("%s exists after rendering only a Workflow Definition (stat err %v)", p, err)
		}
	}
}

// A dry run reports the same actions with or without a Workflow
// Definition in the resolved set.
func TestRender_DryRunReportsNothingForAWorkflowDefinition(t *testing.T) {
	tmp := t.TempDir()

	var without, with bytes.Buffer
	opts := projectOpts(tmp)
	opts.DryRun = true

	opts.Stdout = &without
	if err := codex.Render(makePlan(everyRenderingCategory(), "default"), opts); err != nil {
		t.Fatalf("dry run without workflow: %v", err)
	}
	opts.Stdout = &with
	if err := codex.Render(makePlan(withWorkflow(everyRenderingCategory()), "default"), opts); err != nil {
		t.Fatalf("dry run with workflow: %v", err)
	}

	if without.String() != with.String() {
		t.Errorf("dry run output differs:\nwithout %q\nwith    %q", without.String(), with.String())
	}
}

// Rendering with a Workflow Definition and then without it neither strands
// nor prunes anything another category rendered.
func TestRender_DroppingAWorkflowDefinitionPrunesNothing(t *testing.T) {
	tmp := t.TempDir()
	base := makePlan(everyRenderingCategory(), "default")

	if err := codex.Render(base, projectOpts(tmp)); err != nil {
		t.Fatalf("Render: %v", err)
	}
	before := snapshotTree(t, tmp)

	if err := codex.Render(makePlan(withWorkflow(everyRenderingCategory()), "default"), projectOpts(tmp)); err != nil {
		t.Fatalf("Render with workflow: %v", err)
	}
	if err := codex.Render(base, projectOpts(tmp)); err != nil {
		t.Fatalf("Render without workflow: %v", err)
	}

	assertSameTree(t, before, snapshotTree(t, tmp))
}
