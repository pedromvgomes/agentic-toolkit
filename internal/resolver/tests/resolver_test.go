package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
)

// ===== bare-name resolution =====

func TestResolve_BareSkillFromEntry(t *testing.T) {
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
			"skills": {"challenge"},
		}),
		"definitions/skills/challenge/SKILL.md": validSkillBody("Challenge skill"),
	})
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}

	plan, err := resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", newFakeProvider())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions len = %d, want 1", len(plan.Definitions))
	}
	d := plan.Definitions[0]
	if d.Category != definitions.CategorySkill {
		t.Errorf("category = %s, want skill", d.Category)
	}
	if d.Name != "challenge" {
		t.Errorf("name = %q, want challenge", d.Name)
	}
	if d.StackName != "" {
		t.Errorf("stack = %q, want \"\" (entry-point)", d.StackName)
	}
}

func TestResolve_BareWithCustomRoot(t *testing.T) {
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": "root: ./agentic\n" + stackBody(nil, map[string][]string{
			"skills": {"foo"},
		}),
		"agentic/skills/foo/SKILL.md": validSkillBody("Foo skill"),
	})
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}

	plan, err := resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", newFakeProvider())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Name != "foo" {
		t.Errorf("definitions = %+v", plan.Definitions)
	}
}

// ===== local-path resolution =====

func TestResolve_PathSkill(t *testing.T) {
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
			"skills": {"./elsewhere/foo"},
		}),
		"elsewhere/foo/SKILL.md": validSkillBody("Foo skill"),
	})
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}

	plan, err := resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", newFakeProvider())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Name != "foo" {
		t.Errorf("definitions = %+v", plan.Definitions)
	}
}

func TestResolve_PathRule(t *testing.T) {
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
			"rules": {"./team/style.md"},
		}),
		"team/style.md": validRuleBody("Team style"),
	})
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}

	plan, err := resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", newFakeProvider())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Category != definitions.CategoryRule {
		t.Errorf("definitions = %+v", plan.Definitions)
	}
}

// ===== external URL resolution =====

func TestResolve_URLSkillBundle(t *testing.T) {
	repoFS := makeMapFS(map[string]string{
		"skills/upstream/SKILL.md": validSkillBody("Upstream skill"),
	})
	provider := newFakeProvider().register("github.com/foo/bar.git", "main", repoFS)

	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
			"skills": {"github.com/foo/bar.git/skills/upstream@main"},
		}),
	})
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}

	plan, err := resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", provider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Name != "upstream" {
		t.Fatalf("definitions = %+v", plan.Definitions)
	}
	if plan.Definitions[0].SourceURL != "github.com/foo/bar.git" {
		t.Errorf("source url = %q", plan.Definitions[0].SourceURL)
	}
	if len(plan.Sources) != 1 || plan.Sources[0].Kind != resolver.SourceDefinition {
		t.Errorf("sources = %+v", plan.Sources)
	}
}

func TestResolve_URLRuleFile(t *testing.T) {
	repoFS := makeMapFS(map[string]string{
		"rules/style.md": validRuleBody("Upstream style"),
	})
	provider := newFakeProvider().register("github.com/foo/bar.git", "v1", repoFS)

	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
			"rules": {"github.com/foo/bar.git/rules/style.md@v1"},
		}),
	})
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}

	plan, err := resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", provider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Category != definitions.CategoryRule {
		t.Fatalf("definitions = %+v", plan.Definitions)
	}
}

// ===== extends DAG =====

func TestResolve_ExtendsExternalStack(t *testing.T) {
	upstreamFS := makeMapFS(map[string]string{
		"stacks/default.yaml": stackBody(nil, map[string][]string{
			"skills": {"upstream"},
		}),
		"definitions/skills/upstream/SKILL.md": validSkillBody("Upstream skill"),
	})
	provider := newFakeProvider().register("github.com/foo/bar.git", "main", upstreamFS)

	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": stackBody(
			[]string{"github.com/foo/bar.git/stacks/default.yaml@main"},
			nil,
		),
	})
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}

	plan, err := resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", provider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Name != "upstream" {
		t.Fatalf("definitions = %+v", plan.Definitions)
	}
	// Stack order: child first, entry-point last.
	if len(plan.StackOrder) != 2 {
		t.Fatalf("stack order = %+v", plan.StackOrder)
	}
	if plan.StackOrder[1] != "" {
		t.Errorf("entry-point should be last in stack order, got %v", plan.StackOrder)
	}
	// Sources include the imported stack source.
	if len(plan.Sources) != 1 || plan.Sources[0].URL != "github.com/foo/bar.git" {
		t.Errorf("sources = %+v", plan.Sources)
	}
	if plan.Sources[0].Kind != resolver.SourceStack {
		t.Errorf("source kind = %v, want SourceStack", plan.Sources[0].Kind)
	}
}

func TestResolve_ExtendsLocalStack(t *testing.T) {
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": stackBody(
			[]string{"./stacks/team.yaml"},
			nil,
		),
		"stacks/team.yaml": stackBody(nil, map[string][]string{
			"skills": {"team-skill"},
		}),
		"definitions/skills/team-skill/SKILL.md": validSkillBody("Team skill"),
	})
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}

	plan, err := resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", newFakeProvider())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Name != "team-skill" {
		t.Errorf("definitions = %+v", plan.Definitions)
	}
}

// ===== override semantics =====

func TestResolve_EntryPointWinsOverExtends(t *testing.T) {
	upstreamFS := makeMapFS(map[string]string{
		"stacks/default.yaml": stackBody(nil, map[string][]string{
			"skills": {"upstream"},
		}),
		"definitions/skills/upstream/SKILL.md": validSkillBody("Upstream version"),
	})
	provider := newFakeProvider().register("github.com/foo/bar.git", "main", upstreamFS)

	// Entry-point overrides "upstream" with its own local definition.
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": stackBody(
			[]string{"github.com/foo/bar.git/stacks/default.yaml@main"},
			map[string][]string{"skills": {"upstream"}},
		),
		"definitions/skills/upstream/SKILL.md": validSkillBody("Override version"),
	})
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}

	plan, err := resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", provider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %+v", plan.Definitions)
	}
	skill := plan.Definitions[0].Definition.(*definitions.Skill)
	if skill.Description != "Override version" {
		t.Errorf("description = %q, want \"Override version\"", skill.Description)
	}
	if plan.Definitions[0].StackName != "" {
		t.Errorf("winner should be from entry-point, got stack %q", plan.Definitions[0].StackName)
	}
	// One DiagOverride is expected.
	overrideCount := 0
	for _, d := range plan.Diagnostics {
		if d.Kind == resolver.DiagOverride {
			overrideCount++
		}
	}
	if overrideCount != 1 {
		t.Errorf("override diagnostics = %d, want 1", overrideCount)
	}
}

// ===== cycle detection =====

func TestResolve_CycleInExtends(t *testing.T) {
	// Two repos extend each other.
	repoAFS := makeMapFS(map[string]string{
		"stacks/a.yaml": stackBody(
			[]string{"github.com/repo/b.git/stacks/b.yaml@main"},
			nil,
		),
	})
	repoBFS := makeMapFS(map[string]string{
		"stacks/b.yaml": stackBody(
			[]string{"github.com/repo/a.git/stacks/a.yaml@main"},
			nil,
		),
	})
	provider := newFakeProvider().
		register("github.com/repo/a.git", "main", repoAFS).
		register("github.com/repo/b.git", "main", repoBFS)

	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": stackBody(
			[]string{"github.com/repo/a.git/stacks/a.yaml@main"},
			nil,
		),
	})
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}

	_, err = resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", provider)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error should mention cycle, got: %v", err)
	}
}

// ===== source ordering =====

func TestResolve_SourcesOrderedStacksFirst(t *testing.T) {
	stackRepoFS := makeMapFS(map[string]string{
		"stacks/default.yaml": stackBody(nil, map[string][]string{
			"skills": {"github.com/defs/x.git/skills/foo@main"},
		}),
	})
	defsRepoFS := makeMapFS(map[string]string{
		"skills/foo/SKILL.md": validSkillBody("Foo"),
	})
	provider := newFakeProvider().
		register("github.com/stk/repo.git", "main", stackRepoFS).
		register("github.com/defs/x.git", "main", defsRepoFS)

	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": stackBody(
			[]string{"github.com/stk/repo.git/stacks/default.yaml@main"},
			nil,
		),
	})
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}

	plan, err := resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", provider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Sources) != 2 {
		t.Fatalf("sources = %+v", plan.Sources)
	}
	if plan.Sources[0].Kind != resolver.SourceStack {
		t.Errorf("first source should be stack, got %v", plan.Sources[0].Kind)
	}
	if plan.Sources[1].Kind != resolver.SourceDefinition {
		t.Errorf("second source should be definition, got %v", plan.Sources[1].Kind)
	}
}
