package tests

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/claude"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// everyRenderingCategory is one definition of each category that writes
// something under .claude/ or into CLAUDE.md.
func everyRenderingCategory() []resolver.PlannedDefinition {
	skillFS := makeFS(map[string]string{
		"definitions/skills/foo/SKILL.md":         "ignored\n",
		"definitions/skills/foo/prompts/extra.md": "extra prompt\n",
	})
	agentFS := makeFS(map[string]string{
		"definitions/agents/bar/AGENT.md": "ignored\n",
	})
	return []resolver.PlannedDefinition{
		pdSkill("foo", "skill desc", "skill body\n", "definitions/skills/foo", "default", skillFS),
		pdAgent("bar", "agent desc", "agent body\n", "definitions/agents/bar", "default", agentFS),
		pdCommand("git/commit", "commit cmd", "commit body\n", "default"),
		pdRule("style", "style rule", "style body\n", "default"),
		pdInstruction("plan-approval", "approve plans", "approve plans body", "default"),
		pdHook("safety", "block rm", "PreToolUse", "Bash", "echo blocked", "default"),
		pdMCPStdio("git", "git mcp", "git-mcp", []string{"--repo", "."}, "default"),
		pdSetting("perms", "deny dangerous", map[string]any{
			"permissions": map[string]any{"deny": []any{"Bash(rm -rf:*)"}},
		}, "default"),
	}
}

func withWorkflow(defs []resolver.PlannedDefinition) []resolver.PlannedDefinition {
	return append(append([]resolver.PlannedDefinition{}, defs...), pdWorkflow("triage-issue", "Triage an issue", "default"))
}

func projectOpts(tmp string) claude.Options {
	return claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   filepath.Join(tmp, ".claude"),
		ProjectRoot: tmp,
	}
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

// A Workflow Definition in the resolved set adds no file, managed key or
// manifest entry, and every other category renders exactly as it does
// without it.
func TestRender_WorkflowDefinitionWritesNothing(t *testing.T) {
	without := t.TempDir()
	with := t.TempDir()

	if err := claude.Render(makePlan(everyRenderingCategory(), "default"), projectOpts(without)); err != nil {
		t.Fatalf("Render without workflow: %v", err)
	}
	if err := claude.Render(makePlan(withWorkflow(everyRenderingCategory()), "default"), projectOpts(with)); err != nil {
		t.Fatalf("Render with workflow: %v", err)
	}

	assertSameTree(t, snapshotTree(t, without), snapshotTree(t, with))
}

// A resolved set holding only a Workflow Definition renders the same tree
// as an empty one.
func TestRender_WorkflowDefinitionAloneRendersLikeAnEmptyPlan(t *testing.T) {
	empty := t.TempDir()
	only := t.TempDir()

	if err := claude.Render(makePlan(nil, "default"), projectOpts(empty)); err != nil {
		t.Fatalf("Render empty: %v", err)
	}
	if err := claude.Render(makePlan(withWorkflow(nil), "default"), projectOpts(only)); err != nil {
		t.Fatalf("Render workflow only: %v", err)
	}

	assertSameTree(t, snapshotTree(t, empty), snapshotTree(t, only))
}

// A dry run reports the same actions with or without a Workflow
// Definition in the resolved set.
func TestRender_DryRunReportsNothingForAWorkflowDefinition(t *testing.T) {
	tmp := t.TempDir()

	var without, with bytes.Buffer
	opts := projectOpts(tmp)
	opts.DryRun = true

	opts.Stdout = &without
	if err := claude.Render(makePlan(everyRenderingCategory(), "default"), opts); err != nil {
		t.Fatalf("dry run without workflow: %v", err)
	}
	opts.Stdout = &with
	if err := claude.Render(makePlan(withWorkflow(everyRenderingCategory()), "default"), opts); err != nil {
		t.Fatalf("dry run with workflow: %v", err)
	}

	if without.String() != with.String() {
		t.Errorf("dry run output differs:\nwithout %q\nwith    %q", without.String(), with.String())
	}
}

// Adding a Workflow Definition to a rendered set is no drift, and removing
// it again neither strands nor prunes anything.
func TestDiff_WorkflowDefinitionIsNoDrift(t *testing.T) {
	tmp := t.TempDir()
	base := makePlan(everyRenderingCategory(), "default")
	extended := makePlan(withWorkflow(everyRenderingCategory()), "default")

	if err := claude.Render(base, projectOpts(tmp)); err != nil {
		t.Fatalf("Render: %v", err)
	}

	baseDrift, err := claude.Diff(base, projectOpts(tmp))
	if err != nil {
		t.Fatalf("Diff base: %v", err)
	}
	d, err := claude.Diff(extended, projectOpts(tmp))
	if err != nil {
		t.Fatalf("Diff with workflow: %v", err)
	}
	if !d.Clean() {
		t.Errorf("a Workflow Definition reported drift: %+v", d)
	}
	if d.HasInstructions != baseDrift.HasInstructions || d.HasSettings != baseDrift.HasSettings {
		t.Errorf("a Workflow Definition changed the managed-file hints: base %+v, with %+v", baseDrift, d)
	}

	before := snapshotTree(t, tmp)
	if err := claude.Render(extended, projectOpts(tmp)); err != nil {
		t.Fatalf("Render with workflow: %v", err)
	}
	if err := claude.Render(base, projectOpts(tmp)); err != nil {
		t.Fatalf("Render without workflow: %v", err)
	}
	assertSameTree(t, before, snapshotTree(t, tmp))

	if d, err := claude.Diff(base, projectOpts(tmp)); err != nil || !d.Clean() {
		t.Errorf("Diff after dropping the workflow: drift %+v, err %v", d, err)
	}
}

// A resolved set holding only a Workflow Definition reports no drift and
// no managed-file hint.
func TestDiff_WorkflowDefinitionAloneTouchesNoManagedFile(t *testing.T) {
	tmp := t.TempDir()
	plan := makePlan(withWorkflow(nil), "default")

	d, err := claude.Diff(plan, projectOpts(tmp))
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !d.Clean() || d.HasInstructions || d.HasSettings {
		t.Errorf("a Workflow Definition alone reported %+v", d)
	}
}
