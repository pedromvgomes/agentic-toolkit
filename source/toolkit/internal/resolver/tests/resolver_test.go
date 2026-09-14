package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// ===== bare-name resolution =====

func TestResolve_BareSkillFromStack(t *testing.T) {
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": entryBody([]string{"./stack.yaml"}, ""),
		"stack.yaml": stackBody(nil, map[string][]string{
			"skills": {"challenge"},
		}),
		"definitions/skills/challenge/SKILL.md": validSkillBody("Challenge skill"),
	})

	plan, err := resolveEntry(t, entryFS, newFakeProvider())
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
	if d.StackName != "local:stack.yaml" {
		t.Errorf("stack = %q, want the stack that listed it", d.StackName)
	}
}

func TestResolve_BareWithCustomRoot(t *testing.T) {
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": entryBody([]string{"./stack.yaml"}, ""),
		"stack.yaml": "root: ./catalog\n" + stackBody(nil, map[string][]string{
			"skills": {"foo"},
		}),
		"catalog/skills/foo/SKILL.md": validSkillBody("Foo skill"),
	})

	plan, err := resolveEntry(t, entryFS, newFakeProvider())
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
		".agentic-toolkit.yaml": entryBody([]string{"./stack.yaml"}, ""),
		"stack.yaml": stackBody(nil, map[string][]string{
			"skills": {"./elsewhere/foo"},
		}),
		"elsewhere/foo/SKILL.md": validSkillBody("Foo skill"),
	})

	plan, err := resolveEntry(t, entryFS, newFakeProvider())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Name != "foo" {
		t.Errorf("definitions = %+v", plan.Definitions)
	}
}

func TestResolve_PathRule(t *testing.T) {
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": entryBody([]string{"./stack.yaml"}, ""),
		"stack.yaml": stackBody(nil, map[string][]string{
			"rules": {"./team/style.md"},
		}),
		"team/style.md": validRuleBody("Team style"),
	})

	plan, err := resolveEntry(t, entryFS, newFakeProvider())
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
		".agentic-toolkit.yaml": entryBody([]string{"./stack.yaml"}, ""),
		"stack.yaml": stackBody(nil, map[string][]string{
			"skills": {"github.com/foo/bar.git/skills/upstream@main"},
		}),
	})

	plan, err := resolveEntry(t, entryFS, provider)
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
		".agentic-toolkit.yaml": entryBody([]string{"./stack.yaml"}, ""),
		"stack.yaml": stackBody(nil, map[string][]string{
			"rules": {"github.com/foo/bar.git/rules/style.md@v1"},
		}),
	})

	plan, err := resolveEntry(t, entryFS, provider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Category != definitions.CategoryRule {
		t.Fatalf("definitions = %+v", plan.Definitions)
	}
}

// ===== stacks: DAG =====

func TestResolve_ComposesExternalStack(t *testing.T) {
	upstreamFS := makeMapFS(map[string]string{
		"stacks/default.yaml": stackBody(nil, map[string][]string{
			"skills": {"upstream"},
		}),
		"definitions/skills/upstream/SKILL.md": validSkillBody("Upstream skill"),
	})
	provider := newFakeProvider().register("github.com/foo/bar.git", "main", upstreamFS)

	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": entryBody([]string{"github.com/foo/bar.git/stacks/default.yaml@main"}, ""),
	})

	plan, err := resolveEntry(t, entryFS, provider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Name != "upstream" {
		t.Fatalf("definitions = %+v", plan.Definitions)
	}
	// Stack order: composed stack first, entry manifest last.
	if len(plan.StackOrder) != 2 {
		t.Fatalf("stack order = %+v", plan.StackOrder)
	}
	if plan.StackOrder[1] != "" {
		t.Errorf("the entry manifest should be last in stack order, got %v", plan.StackOrder)
	}
	// Sources include the composed stack's source.
	if len(plan.Sources) != 1 || plan.Sources[0].URL != "github.com/foo/bar.git" {
		t.Errorf("sources = %+v", plan.Sources)
	}
	if plan.Sources[0].Kind != resolver.SourceStack {
		t.Errorf("source kind = %v, want SourceStack", plan.Sources[0].Kind)
	}
}

func TestResolve_ComposesLocalStack(t *testing.T) {
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": entryBody([]string{"./stacks/team.yaml"}, ""),
		"stacks/team.yaml": stackBody(nil, map[string][]string{
			"skills": {"team-skill"},
		}),
		"definitions/skills/team-skill/SKILL.md": validSkillBody("Team skill"),
	})

	plan, err := resolveEntry(t, entryFS, newFakeProvider())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Name != "team-skill" {
		t.Errorf("definitions = %+v", plan.Definitions)
	}
}

// A stack composed by the entry manifest brings its own `extends:` with it,
// and those apply before it does.
func TestResolve_ComposedStackBringsItsOwnExtends(t *testing.T) {
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": entryBody([]string{"./stacks/team.yaml"}, ""),
		"stacks/team.yaml":      stackBody([]string{"./base.yaml"}, nil),
		"stacks/base.yaml": stackBody(nil, map[string][]string{
			"skills": {"base-skill"},
		}),
		"definitions/skills/base-skill/SKILL.md": validSkillBody("Base skill"),
	})

	plan, err := resolveEntry(t, entryFS, newFakeProvider())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Name != "base-skill" {
		t.Fatalf("definitions = %+v", plan.Definitions)
	}
	want := []string{"local:stacks/base.yaml", "local:stacks/team.yaml", ""}
	if len(plan.StackOrder) != len(want) {
		t.Fatalf("stack order = %v, want %v", plan.StackOrder, want)
	}
	for i, id := range want {
		if plan.StackOrder[i] != id {
			t.Errorf("stack order = %v, want %v", plan.StackOrder, want)
			break
		}
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
		".agentic-toolkit.yaml": entryBody([]string{"github.com/repo/a.git/stacks/a.yaml@main"}, ""),
	})

	_, err := resolveEntry(t, entryFS, provider)
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
		".agentic-toolkit.yaml": entryBody([]string{"github.com/stk/repo.git/stacks/default.yaml@main"}, ""),
	})

	plan, err := resolveEntry(t, entryFS, provider)
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
