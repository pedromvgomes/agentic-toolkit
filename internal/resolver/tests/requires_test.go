package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
)

// bodyRequiring is a definition body that declares cross-references.
func bodyRequiring(description string, requires ...string) string {
	b := "---\ndescription: " + description + "\nrequires:\n"
	for _, r := range requires {
		b += "  - " + r + "\n"
	}
	return b + "---\n\nbody\n"
}

func resolvePlan(t *testing.T, files map[string]string) *resolver.Plan {
	t.Helper()

	entryFS := makeMapFS(files)
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}
	plan, err := resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", newFakeProvider())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return plan
}

func planHas(plan *resolver.Plan, cat definitions.Category, name string) bool {
	for _, d := range plan.Definitions {
		if d.Category == cat && d.Name == name {
			return true
		}
	}
	return false
}

func diagKinds(plan *resolver.Plan, kind resolver.DiagnosticKind) []string {
	var out []string
	for _, d := range plan.Diagnostics {
		if d.Kind == kind {
			out = append(out, d.Message)
		}
	}
	return out
}

// A definition's `requires:` is its author saying it does not work alone — a
// skill that dispatches to a subagent, an instruction that names one. A stack
// listing the skill and not the agent renders something that fails at the
// moment it delegates, which is how wrap-session shipped broken to every
// consumer.
func TestARequiredDefinitionIsPulledInWhenNoStackListsIt(t *testing.T) {
	plan := resolvePlan(t, map[string]string{
		".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
			"skills": {"wrap-session"},
		}),
		"definitions/skills/wrap-session/SKILL.md":          bodyRequiring("Wraps a session", "agents/wrap-session-reviewer"),
		"definitions/agents/wrap-session-reviewer/AGENT.md": validAgentBody("Reviews a session"),
	})

	if !planHas(plan, definitions.CategoryAgent, "wrap-session-reviewer") {
		t.Fatalf("the required agent was not pulled in: %+v", plan.Definitions)
	}
	if got := diagKinds(plan, resolver.DiagPulledRequirement); len(got) != 1 {
		t.Errorf("diagnostics = %v, want one saying it was pulled in", got)
	}
}

// Requirements are transitive: the definition pulled in may declare its own,
// and stopping after one round would leave the second dependency missing with
// nothing said about it.
func TestRequirementsAreFollowedTransitively(t *testing.T) {
	plan := resolvePlan(t, map[string]string{
		".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
			"skills": {"wrap-session"},
		}),
		"definitions/skills/wrap-session/SKILL.md":          bodyRequiring("Wraps a session", "agents/wrap-session-reviewer"),
		"definitions/agents/wrap-session-reviewer/AGENT.md": bodyRequiring("Reviews a session", "skills/agents-md-creator"),
		"definitions/skills/agents-md-creator/SKILL.md":     validSkillBody("Creates AGENTS.md"),
	})

	for _, want := range []struct {
		cat  definitions.Category
		name string
	}{
		{definitions.CategoryAgent, "wrap-session-reviewer"},
		{definitions.CategorySkill, "agents-md-creator"},
	} {
		if !planHas(plan, want.cat, want.name) {
			t.Errorf("%s/%s was not pulled in: %+v", want.cat, want.name, plan.Definitions)
		}
	}
}

// A stack that already lists what a definition requires must resolve exactly
// as it did before, with nothing added and nothing announced — otherwise every
// well-formed stack starts reporting diagnostics about itself.
func TestAStackThatListsItsRequirementsIsUnchanged(t *testing.T) {
	plan := resolvePlan(t, map[string]string{
		".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
			"skills": {"wrap-session"},
			"agents": {"wrap-session-reviewer"},
		}),
		"definitions/skills/wrap-session/SKILL.md":          bodyRequiring("Wraps a session", "agents/wrap-session-reviewer"),
		"definitions/agents/wrap-session-reviewer/AGENT.md": validAgentBody("Reviews a session"),
	})

	if len(plan.Definitions) != 2 {
		t.Errorf("definitions = %d, want the two the stack listed", len(plan.Definitions))
	}
	if got := diagKinds(plan, resolver.DiagPulledRequirement); len(got) != 0 {
		t.Errorf("diagnostics = %v, want none for a stack that lists what it needs", got)
	}
}

// A requirement naming something that does not exist is somebody else's
// metadata problem, usually in a definition the consumer does not own.
// Failing their sync over it would be worse than rendering without it, which
// is what happens today anyway — but it must not pass in silence.
func TestAnUnresolvableRequirementIsReportedAndNotFatal(t *testing.T) {
	plan := resolvePlan(t, map[string]string{
		".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
			"skills": {"wrap-session"},
		}),
		"definitions/skills/wrap-session/SKILL.md": bodyRequiring("Wraps a session", "agents/does-not-exist"),
	})

	if !planHas(plan, definitions.CategorySkill, "wrap-session") {
		t.Fatal("the skill itself was dropped over a bad requirement")
	}
	got := diagKinds(plan, resolver.DiagUnresolvedRequirement)
	if len(got) != 1 {
		t.Fatalf("diagnostics = %v, want one reporting the unresolved requirement", got)
	}
	if !strings.Contains(got[0], "does-not-exist") {
		t.Errorf("the diagnostic does not name the requirement: %q", got[0])
	}
}

// A malformed cross-reference must be reported rather than silently ignored,
// for the reason `requires:` exists at all: nothing else in the pipeline reads
// it, so a typo would otherwise be inert forever.
func TestAMalformedRequirementIsReported(t *testing.T) {
	plan := resolvePlan(t, map[string]string{
		".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
			"skills": {"wrap-session"},
		}),
		"definitions/skills/wrap-session/SKILL.md": bodyRequiring("Wraps a session", "wrap-session-reviewer"),
	})

	if got := diagKinds(plan, resolver.DiagUnresolvedRequirement); len(got) != 1 {
		t.Errorf("diagnostics = %v, want one reporting the malformed reference", got)
	}
}

// Two definitions needing the same third one is ordinary. Announcing it twice
// would make the diagnostics read as though something were wrong.
func TestARequirementSharedByTwoDefinitionsIsAnnouncedOnce(t *testing.T) {
	plan := resolvePlan(t, map[string]string{
		".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
			"skills": {"one", "two"},
		}),
		"definitions/skills/one/SKILL.md":    bodyRequiring("One", "agents/shared"),
		"definitions/skills/two/SKILL.md":    bodyRequiring("Two", "agents/shared"),
		"definitions/agents/shared/AGENT.md": validAgentBody("Shared"),
	})

	if got := diagKinds(plan, resolver.DiagPulledRequirement); len(got) != 1 {
		t.Errorf("diagnostics = %v, want exactly one", got)
	}
}

// A requirement cycle is a mistake, not a crash. The fixpoint must settle
// rather than pull forever.
func TestARequirementCycleSettles(t *testing.T) {
	plan := resolvePlan(t, map[string]string{
		".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
			"skills": {"a"},
		}),
		"definitions/skills/a/SKILL.md": bodyRequiring("A", "skills/b"),
		"definitions/skills/b/SKILL.md": bodyRequiring("B", "skills/a"),
	})

	for _, name := range []string{"a", "b"} {
		if !planHas(plan, definitions.CategorySkill, name) {
			t.Errorf("skills/%s missing: %+v", name, plan.Definitions)
		}
	}
}
