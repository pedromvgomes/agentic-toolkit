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
// moment it delegates.
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

// A requirement naming something that does not exist is a metadata problem in
// a definition the consumer often does not own. Failing their whole resolve
// over it is a worse outcome than rendering without it — but it must not pass
// in silence.
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

// A reference that is not in 'category/name' form resolves to nothing, so it
// must surface as a diagnostic rather than being skipped in silence: the
// declared dependency is absent either way, and only the diagnostic says so.
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

// A requirement is resolved through the source and convention root of the
// definition that declared it, not the stack that listed it. A remote
// definition's dependency lives in the remote repo, and looking it up in the
// consumer's tree both fails to find it and — worse, if a name happens to
// collide — renders whatever the consumer had under that name while
// attributing it to the remote source.
func TestARemoteDefinitionsRequirementResolvesInsideItsOwnSource(t *testing.T) {
	remote := makeMapFS(map[string]string{
		"skills/wrap-session/SKILL.md":          bodyRequiring("Wraps a session", "agents/wrap-session-reviewer"),
		"agents/wrap-session-reviewer/AGENT.md": validAgentBody("Reviews a session"),
	})
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
			"skills": {"github.com/o/r.git/skills/wrap-session"},
		}),
		// A same-named agent in the consumer's own tree. Resolving the remote
		// definition's requirement here would find this one.
		"definitions/agents/wrap-session-reviewer/AGENT.md": validAgentBody("The consumer's own agent"),
	})
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}
	provider := newFakeProvider().register("github.com/o/r.git", "", remote)

	plan, err := resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", provider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	var pulled *resolver.PlannedDefinition
	for i, d := range plan.Definitions {
		if d.Category == definitions.CategoryAgent && d.Name == "wrap-session-reviewer" {
			pulled = &plan.Definitions[i]
		}
	}
	if pulled == nil {
		t.Fatalf("the required agent was not pulled in: %+v", plan.Definitions)
	}
	if pulled.SourceURL != "github.com/o/r.git" {
		t.Errorf("pulled agent came from %q, want the source that declared the requirement", pulled.SourceURL)
	}
	if strings.Contains(pulled.Definition.GetCommon().Description, "consumer's own") {
		t.Error("a remote definition's requirement resolved against the consumer's own tree")
	}
}

// The name half of a requirement reaches path.Join, which cleans "..". Without
// the same validation a manifest entry gets, a definition could name a file
// outside the convention root entirely — and a definition's frontmatter is
// authored wherever the definition came from.
func TestARequirementCannotEscapeTheConventionRoot(t *testing.T) {
	for name, req := range map[string]string{
		"parent traversal":     "skills/../../elsewhere/evil",
		"absolute":             "skills//etc/passwd",
		"dot segment":          "skills/./evil",
		"slash in non-command": "agents/nested/evil",
	} {
		t.Run(name, func(t *testing.T) {
			plan := resolvePlan(t, map[string]string{
				".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
					"skills": {"wrap-session"},
				}),
				"definitions/skills/wrap-session/SKILL.md": bodyRequiring("Wraps a session", req),
				"elsewhere/evil/SKILL.md":                  validSkillBody("Should be unreachable"),
				"definitions/skills/evil/SKILL.md":         validSkillBody("Should be unreachable"),
			})

			for _, d := range plan.Definitions {
				if strings.Contains(d.Definition.GetCommon().Description, "unreachable") {
					t.Fatalf("a requirement reached outside the convention root: %+v", d)
				}
			}
			if got := diagKinds(plan, resolver.DiagUnresolvedRequirement); len(got) != 1 {
				t.Errorf("diagnostics = %v, want one refusing the reference", got)
			}
		})
	}
}

// A requirement two definitions declare is one missing definition, not two
// problems.
func TestASharedUnresolvableRequirementIsReportedOnce(t *testing.T) {
	plan := resolvePlan(t, map[string]string{
		".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
			"skills": {"one", "two"},
		}),
		"definitions/skills/one/SKILL.md": bodyRequiring("One", "agents/missing"),
		"definitions/skills/two/SKILL.md": bodyRequiring("Two", "agents/missing"),
	})

	if got := diagKinds(plan, resolver.DiagUnresolvedRequirement); len(got) != 1 {
		t.Errorf("diagnostics = %v, want exactly one", got)
	}
}

// One definition failing to resolve a requirement must not settle it for
// everyone: another may declare the same thing from a source where it exists.
// Blaming the first requirer for something that ends up present is the failure
// deferred reporting exists to avoid.
func TestARequirementOneDefinitionCannotResolveIsStillSatisfiedByAnother(t *testing.T) {
	remote := makeMapFS(map[string]string{
		"skills/needs-it/SKILL.md": bodyRequiring("Needs it", "agents/shared"),
	})
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
			"skills": {"github.com/o/r.git/skills/needs-it", "local-needs-it"},
		}),
		"definitions/skills/local-needs-it/SKILL.md": bodyRequiring("Also needs it", "agents/shared"),
		"definitions/agents/shared/AGENT.md":         validAgentBody("Shared"),
	})
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}
	provider := newFakeProvider().register("github.com/o/r.git", "", remote)

	plan, err := resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", provider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	found := false
	for _, d := range plan.Definitions {
		if d.Category == definitions.CategoryAgent && d.Name == "shared" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the requirement was never satisfied: %+v", plan.Definitions)
	}
	if got := diagKinds(plan, resolver.DiagUnresolvedRequirement); len(got) != 0 {
		t.Errorf("diagnostics = %v, want none once the requirement is satisfied", got)
	}
}

// A file-shaped definition's `name:` field wins over its filename, so the
// definition a requirement resolves to may not be called what the requirement
// called it. Keying the overlay on the requirement's spelling would let the
// same definition land twice.
func TestAPulledDefinitionIsKeyedByItsOwnName(t *testing.T) {
	plan := resolvePlan(t, map[string]string{
		".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
			"skills":       {"one"},
			"instructions": {"renamed"},
		}),
		"definitions/skills/one/SKILL.md":     bodyRequiring("One", "instructions/renamed"),
		"definitions/instructions/renamed.md": "---\nname: actual-name\ndescription: An instruction\n---\n\nbody\n",
	})

	count := 0
	for _, d := range plan.Definitions {
		if d.Category == definitions.CategoryInstruction {
			count++
		}
	}
	if count != 1 {
		t.Errorf("instruction count = %d, want 1 — the same definition was keyed twice", count)
	}
}
