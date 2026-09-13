package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

// A repo can adopt the memory store without the feature flow, so the memory
// stack has to render on its own: the explorer delegations are routed to, the
// commands that curate and seed, and the grants those need.
//
// The grants are the part that only a render can show. `addMemoryGrants`
// appends the store paths onto an allow list some definition already
// contributed — so a stack shipping the explorer and no permissions at all
// renders an explorer that prompts on every delegation, and nothing in the
// catalog says so.
func TestTheMemoryStackRendersOnItsOwn(t *testing.T) {
	apply := renderStack(t, "memory")

	for _, path := range []string{
		".claude/agents/memory-explorer/AGENT.md",
		".claude/commands/memory-curate.md",
		".claude/commands/memory-seed.md",
	} {
		if _, err := os.Stat(filepath.Join(apply, path)); err != nil {
			t.Errorf("%s did not reach a memory-only consumer: %v", path, err)
		}
	}

	allow := allowList(t, apply)
	for _, want := range []string{
		"Bash(agtk memory stats*)",
		"Bash(agtk memory show *)",
		"Bash(agtk memory candidates*)",
		"Read(**/" + memory.DefaultRoot + "/INDEX.md)",
		"Edit(**/" + memory.DefaultRoot + "/candidates/**)",
	} {
		if !hasRule(allow, want) {
			t.Errorf("a memory-only consumer pre-approves no %q, so the explorer prompts:\n%v", want, allow)
		}
	}

	// Curation spends money and rewrites notes/, so it stays a prompt wherever
	// the memory definitions are shipped from.
	if hasRule(allow, "Bash(agtk memory curate*)") {
		t.Error("the memory stack pre-approves curate, which must stay a deliberate prompt")
	}
}

// Moving the memory definitions into their own stack must not change what a
// consumer of the default stack gets. The grants are the ones that could
// silently go missing: they now arrive from a stack the default extends, and
// an extending stack's own `permissions` used to replace what it extended.
func TestExtendingTheMemoryStackKeepsEveryDefaultGrant(t *testing.T) {
	allow := allowList(t, renderDefaultStack(t))

	for _, want := range []string{
		// From the memory stack, through extends.
		"Bash(agtk memory stats*)",
		"Bash(agtk memory show *)",
		"Bash(agtk memory candidates*)",
		"Read(**/" + memory.DefaultRoot + "/INDEX.md)",
		"Edit(**/" + memory.DefaultRoot + "/candidates/**)",
		// From the default stack's own settings definition.
		"Bash(agtk code-review panels*)",
		"Bash(agtk code-review explain*)",
		"Bash(agtk code-review signals*)",
		"Bash(gh pr view *)",
	} {
		if !hasRule(allow, want) {
			t.Errorf("the default stack lost %q when memory moved into its own stack:\n%v", want, allow)
		}
	}
}

// allowList reads a rendered consumer's permissions.allow.
func allowList(t *testing.T, apply string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(apply, ".claude/settings.json"))
	if err != nil {
		t.Fatalf("settings did not reach the consumer: %v", err)
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

func hasRule(allow []string, want string) bool {
	for _, a := range allow {
		if a == want {
			return true
		}
	}
	return false
}

// Serena keeps its memories at `.serena/memories/`, writes them through its MCP
// server's own tool, and is governed by `mcp__serena__*` rules rather than
// file-path ones. A `Write(...)` rule is not consulted by the file permission
// check at all, so this grant named a path that does not exist, in a tree the
// codex adapter renders and prunes, through a rule family that grants nothing.
//
// Its absence is the assertion: nothing in the catalog writes Serena memories
// with Claude's file tools, so no respelling of it belongs here either.
func TestNoGrantNamesTheRenderedAgentsTree(t *testing.T) {
	for _, stack := range []string{"default", "memory"} {
		t.Run(stack, func(t *testing.T) {
			for _, rule := range allowList(t, renderStack(t, stack)) {
				if strings.Contains(rule, ".agents/") {
					t.Errorf("%q pre-approves a path inside a rendered tree", rule)
				}
				if strings.HasPrefix(rule, "Write(") {
					t.Errorf("%q is spelled Write(...), which the file permission check does not consult", rule)
				}
			}
		})
	}
}
