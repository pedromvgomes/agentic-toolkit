package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/claude"
	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
)

// renderWithMemoryRoot renders a plan carrying one permissions-contributing
// setting, with the entry manifest's `memory.root` set to root ("" leaves it
// unset), and returns settings.json's allow list.
func renderWithMemoryRoot(t *testing.T, root string) []string {
	t.Helper()
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")

	plan := makePlan([]resolver.PlannedDefinition{
		pdSetting("perms", "deny dangerous", map[string]any{
			"permissions": map[string]any{"allow": []any{"Bash(agtk memory stats*)"}},
		}, "default"),
		pdAgent("memory-explorer", "reads the store", "body\n",
			"definitions/agents/memory-explorer", "default",
			makeFS(map[string]string{"definitions/agents/memory-explorer/AGENT.md": "body\n"})),
	}, "default")
	plan.Stack = &stack.Stack{}
	if root != "" {
		plan.Stack.Memory = &stack.MemoryConfig{Root: root}
	}

	if err := claude.Render(plan, claude.Options{
		Scope: claude.ScopeProject, ScopeRoot: scopeRoot, ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(scopeRoot, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("settings.json: %v\n%s", err, raw)
	}
	return settings.Permissions.Allow
}

func hasGrant(allow []string, want string) bool {
	for _, a := range allow {
		if a == want {
			return true
		}
	}
	return false
}

// A consumer that sets `memory.root` gets grants for the store it actually
// has. A literal in the settings definition could not: the definition is
// shared, and the root is read from the consumer's own entry manifest.
func TestMemoryGrantsFollowAConfiguredRoot(t *testing.T) {
	allow := renderWithMemoryRoot(t, "docs/memory")

	for _, want := range []string{
		"Read(**/docs/memory/INDEX.md)",
		"Edit(**/docs/memory/candidates/**)",
	} {
		if !hasGrant(allow, want) {
			t.Errorf("settings.json pre-approves no %q, so the explorer prompts on every delegation:\n%v", want, allow)
		}
	}
	for _, unwanted := range allow {
		if strings.Contains(unwanted, memory.DefaultRoot+"/") {
			t.Errorf("grant %q names the default root rather than the configured one", unwanted)
		}
	}
}

// An unset root is the default, and the grant has to say so — this is the
// case every consumer that never configured memory lands in.
func TestMemoryGrantsFallBackToTheDefaultRoot(t *testing.T) {
	allow := renderWithMemoryRoot(t, "")

	for _, want := range []string{
		"Read(**/" + memory.DefaultRoot + "/INDEX.md)",
		"Edit(**/" + memory.DefaultRoot + "/candidates/**)",
	} {
		if !hasGrant(allow, want) {
			t.Errorf("settings.json pre-approves no %q:\n%v", want, allow)
		}
	}
}

// `memory.root: .` puts the store at the project root, where there is no
// directory to name. Reusing the `**/<root>/...` shape there drops to
// `Edit(**/candidates/**)` — a write grant on every directory called
// `candidates` anywhere in the tree — so this case is anchored instead.
func TestAProjectRootStoreGrantsAreAnchoredNotWildcarded(t *testing.T) {
	allow := renderWithMemoryRoot(t, ".")

	for _, want := range []string{"Read(INDEX.md)", "Edit(candidates/**)"} {
		if !hasGrant(allow, want) {
			t.Errorf("grants for a project-root store are not anchored:\n%v", allow)
		}
	}
	for _, unwanted := range []string{"Edit(**/candidates/**)", "Read(**/INDEX.md)"} {
		if hasGrant(allow, unwanted) {
			t.Errorf("grant %q reaches every directory of that name in the tree:\n%v", unwanted, allow)
		}
	}
}

// A deny-only contribution is a stack tightening what an agent may do.
// Answering that by creating the allow list it never wrote would turn a
// restriction into a grant.
func TestADenyOnlyStackGainsNoAllowList(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")

	plan := makePlan([]resolver.PlannedDefinition{
		pdSetting("perms", "deny dangerous", map[string]any{
			"permissions": map[string]any{"deny": []any{"Bash(rm -rf:*)"}},
		}, "default"),
	}, "default")
	plan.Stack = &stack.Stack{}

	if err := claude.Render(plan, claude.Options{
		Scope: claude.ScopeProject, ScopeRoot: scopeRoot, ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(scopeRoot, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "allow") {
		t.Errorf("a deny-only stack grew an allow list:\n%s", raw)
	}
}

// The branch that refuses a malformed allow list is the one that chooses to
// fail loudly rather than drop the grants silently, so it needs its own test.
func TestAMalformedAllowListIsRefused(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")

	plan := makePlan([]resolver.PlannedDefinition{
		pdSetting("perms", "malformed", map[string]any{
			"permissions": map[string]any{"allow": "Bash(ls)"},
		}, "default"),
	}, "default")
	plan.Stack = &stack.Stack{}

	err := claude.Render(plan, claude.Options{
		Scope: claude.ScopeProject, ScopeRoot: scopeRoot, ProjectRoot: tmp,
	})
	if err == nil {
		t.Fatal("Render accepted a permissions.allow that is not a list")
	}
	if !strings.Contains(err.Error(), "permissions.allow") {
		t.Errorf("error = %q, want it to name the key", err)
	}
}

// The grants are appended to what the definitions declare, never instead of
// it. Replacing the allow list would drop every other pre-approval and turn
// each one back into a prompt.
func TestMemoryGrantsAreAppendedNotSubstituted(t *testing.T) {
	allow := renderWithMemoryRoot(t, "")

	if !hasGrant(allow, "Bash(agtk memory stats*)") {
		t.Errorf("appending the store grants dropped the definition's own:\n%v", allow)
	}
}

// A stack that pre-approves nothing has said what it wants. Conjuring a
// permissions key here would hand a consumer grants it never asked for.
func TestNoPermissionsKeyMeansNoMemoryGrants(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")

	plan := makePlan([]resolver.PlannedDefinition{
		pdSetting("model", "pick a model", map[string]any{"model": "opus"}, "default"),
	}, "default")
	plan.Stack = &stack.Stack{}

	if err := claude.Render(plan, claude.Options{
		Scope: claude.ScopeProject, ScopeRoot: scopeRoot, ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(scopeRoot, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "permissions") {
		t.Errorf("settings.json grew a permissions key nothing declared:\n%s", raw)
	}
}

// A stack that ships no memory tooling and pre-approves something unrelated
// must not pick up a standing write grant on a store it does not have. The
// append runs after the last-wins merge, so such a consumer could not take
// the key back; the only way left to decline would be a deny rule, which
// states something else.
func TestAStackWithoutMemoryToolingGetsNoStoreGrants(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")

	plan := makePlan([]resolver.PlannedDefinition{
		pdSetting("perms", "allow tests", map[string]any{
			"permissions": map[string]any{"allow": []any{"Bash(cargo test)"}},
		}, "default"),
	}, "default")
	plan.Stack = &stack.Stack{}

	if err := claude.Render(plan, claude.Options{
		Scope: claude.ScopeProject, ScopeRoot: scopeRoot, ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(scopeRoot, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), memory.CandidatesDir) {
		t.Errorf("a stack with no memory tooling was granted the store:\n%s", raw)
	}
}

// A `memory:` block is adoption too: the consumer configured the store, even
// if no definition in this plan reads it.
func TestAConfiguredMemoryBlockIsAdoptionEnough(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")

	plan := makePlan([]resolver.PlannedDefinition{
		pdSetting("perms", "allow tests", map[string]any{
			"permissions": map[string]any{"allow": []any{"Bash(cargo test)"}},
		}, "default"),
	}, "default")
	plan.Stack = &stack.Stack{Memory: &stack.MemoryConfig{Root: "docs/memory"}}

	if err := claude.Render(plan, claude.Options{
		Scope: claude.ScopeProject, ScopeRoot: scopeRoot, ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(scopeRoot, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "docs/memory/INDEX.md") {
		t.Errorf("a configured store was not granted:\n%s", raw)
	}
}

// A root the memory commands refuse must not render into patterns that can
// match no store, which is a prompt on every delegation and no diagnostic.
func TestARootTheMemoryCommandsRefuseFailsTheRender(t *testing.T) {
	for _, bad := range []string{"../outside", "/etc"} {
		t.Run(bad, func(t *testing.T) {
			tmp := t.TempDir()
			plan := makePlan([]resolver.PlannedDefinition{
				pdSetting("perms", "allow tests", map[string]any{
					"permissions": map[string]any{"allow": []any{"Bash(cargo test)"}},
				}, "default"),
			}, "default")
			plan.Stack = &stack.Stack{Memory: &stack.MemoryConfig{Root: bad}}

			err := claude.Render(plan, claude.Options{
				Scope: claude.ScopeProject, ScopeRoot: filepath.Join(tmp, ".claude"), ProjectRoot: tmp,
			})
			if err == nil {
				t.Fatalf("Render accepted memory.root %q", bad)
			}
			if !strings.Contains(err.Error(), "memory.root") {
				t.Errorf("error = %q, want it to name the field", err)
			}
		})
	}
}

// A `Write(...)` rule is not consulted by the file permission check at all, so
// a staging grant spelled that way names the right path and pre-approves
// nothing — the explorer keeps prompting while settings.json says otherwise.
// The curator makes the same check at the other place agtk builds a store
// grant; this is the second.
func TestNoStoreGrantIsSpelledWrite(t *testing.T) {
	for _, root := range []string{"", "docs/memory", "."} {
		for _, grant := range renderWithMemoryRoot(t, root) {
			if strings.HasPrefix(grant, "Write(") {
				t.Errorf("memory.root %q emitted %q, which the permission check never reads", root, grant)
			}
		}
	}
}
