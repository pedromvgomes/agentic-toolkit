package tests

import (
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
)

// TestResolveStack_NoEntryManifest_NoConventionScan pins what a stack-only
// resolve contains: the named stack's entries and nothing else. Convention
// scanning belongs to an entry manifest, so a tree that happens to hold an
// `agentic/` root contributes nothing here.
func TestResolveStack_NoEntryManifest_NoConventionScan(t *testing.T) {
	fsys := makeMapFS(map[string]string{
		"stacks/default.yaml":              stackBody(nil, map[string][]string{"skills": {"tidy"}}),
		"definitions/skills/tidy/SKILL.md": validSkillBody("Tidy skill"),
		"agentic/instructions/repo.md":     validInstructionBody("Repo-scoped instruction"),
	})

	st, err := stack.ParseInFS(fsys, "stacks/default.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}
	plan, err := resolver.ResolveStack(st, fsys, "stacks/default.yaml", newFakeProvider())
	if err != nil {
		t.Fatalf("resolve stack: %v", err)
	}

	if plan.EntryManifest != nil {
		t.Errorf("EntryManifest = %+v, want nil", plan.EntryManifest)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %+v, want only the stack's own entry", plan.Definitions)
	}
	d := plan.Definitions[0]
	if d.Category != definitions.CategorySkill || d.Name != "tidy" {
		t.Errorf("got (%s, %q), want (skill, \"tidy\")", d.Category, d.Name)
	}
}

// TestResolveStack_EffectivePlatforms_DefaultsToClaude covers the platforms:
// field having no manifest to come from.
func TestResolveStack_EffectivePlatforms_DefaultsToClaude(t *testing.T) {
	fsys := makeMapFS(map[string]string{
		"stacks/default.yaml": "root: definitions\n",
	})
	st, err := stack.ParseInFS(fsys, "stacks/default.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}
	plan, err := resolver.ResolveStack(st, fsys, "stacks/default.yaml", newFakeProvider())
	if err != nil {
		t.Fatalf("resolve stack: %v", err)
	}
	got := plan.EffectivePlatforms()
	if len(got) != 1 || got[0] != definitions.PlatformClaude {
		t.Errorf("platforms = %v, want [%s]", got, definitions.PlatformClaude)
	}
}
