package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/claude"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// TestDiff_CleanAfterRender: a freshly-rendered scope reports no drift.
func TestDiff_CleanAfterRender(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")
	plan := makePlan([]resolver.PlannedDefinition{
		pdRule("style", "style rule", "style body\n", "default"),
	}, "default")

	if err := claude.Render(plan, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	d, err := claude.Diff(plan, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !d.Clean() {
		t.Errorf("Diff after Render should be clean, got %+v", d)
	}
}

// TestDiff_ModifiedTrackedFile: editing a rendered file flips Modified.
func TestDiff_ModifiedTrackedFile(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")
	plan := makePlan([]resolver.PlannedDefinition{
		pdRule("style", "style rule", "style body\n", "default"),
	}, "default")

	if err := claude.Render(plan, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	target := filepath.Join(scopeRoot, "rules/style.md")
	if err := os.WriteFile(target, []byte("user-edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d, err := claude.Diff(plan, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if d.Clean() {
		t.Fatalf("expected drift, got clean")
	}
	if !contains(d.Modified, "rules/style.md") {
		t.Errorf("Modified should list rules/style.md, got %v", d.Modified)
	}
}

// TestDiff_MissingTrackedFile: deleting a rendered file flips Missing.
func TestDiff_MissingTrackedFile(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")
	plan := makePlan([]resolver.PlannedDefinition{
		pdRule("style", "style rule", "style body\n", "default"),
	}, "default")

	if err := claude.Render(plan, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if err := os.Remove(filepath.Join(scopeRoot, "rules/style.md")); err != nil {
		t.Fatal(err)
	}

	d, err := claude.Diff(plan, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !contains(d.Missing, "rules/style.md") {
		t.Errorf("Missing should list rules/style.md, got %v", d.Missing)
	}
}

// TestDiff_StalePlanShrinks: a plan that drops a definition leaves the
// previously-rendered file in Stale.
func TestDiff_StalePlanShrinks(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")

	planA := makePlan([]resolver.PlannedDefinition{
		pdRule("style", "style rule", "style body\n", "default"),
		pdRule("naming", "naming rule", "naming body\n", "default"),
	}, "default")
	if err := claude.Render(planA, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render A: %v", err)
	}

	planB := makePlan([]resolver.PlannedDefinition{
		pdRule("style", "style rule", "style body\n", "default"),
	}, "default")
	d, err := claude.Diff(planB, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	})
	if err != nil {
		t.Fatalf("Diff B: %v", err)
	}
	if !contains(d.Stale, "rules/naming.md") {
		t.Errorf("Stale should list rules/naming.md, got %v", d.Stale)
	}
}
