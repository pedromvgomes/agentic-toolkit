package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

// The default stack is the thing consumers actually get, and every part of it
// is prose that no other test reads. A definition can be well-formed, listed,
// and still never reach a consumer — `wrap-session` dispatched to an agent the
// stack did not list, and was broken in every consumer for as long as nobody
// rendered it.
//
// So this renders the real definitions/ and stacks/default.yaml into a
// throwaway consumer and asserts what arrives.
func TestTheDefaultStackRendersEveryThingItLists(t *testing.T) {
	apply := renderDefaultStack(t)

	for _, tc := range []struct {
		path string
		want string
	}{
		{".claude/agents/memory-explorer/AGENT.md", "memory-explorer"},
		// Listed only after it was found dispatched-to and unrendered.
		{".claude/agents/wrap-session-reviewer/AGENT.md", "wrap-session-reviewer"},
		{".claude/commands/memory-curate.md", "agtk memory curate"},
		{".claude/commands/memory-seed.md", "memory-explorer"},
		{".claude/skills/wrap-session/SKILL.md", "wrap-session-reviewer"},
		{".claude/settings.json", "agtk memory candidates"},
	} {
		body, err := os.ReadFile(filepath.Join(apply, tc.path))
		if err != nil {
			t.Errorf("%s did not reach the consumer: %v", tc.path, err)
			continue
		}
		if !strings.Contains(string(body), tc.want) {
			t.Errorf("%s arrived without %q", tc.path, tc.want)
		}
	}
}

// Claude Code reads a slash command's tool allowlist from `allowed-tools`.
// Under any other key the restriction is not rejected, it is ignored — so the
// command runs with the session's whole tool set, and the render looks fine.
func TestACommandsToolAllowlistArrivesUnderTheKeyClaudeReads(t *testing.T) {
	apply := renderDefaultStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/commands/memory-curate.md"))
	if err != nil {
		t.Fatalf("memory-curate did not reach the consumer: %v", err)
	}
	if !strings.Contains(string(body), "allowed-tools:") {
		t.Errorf("the tool allowlist did not render under `allowed-tools`:\n%s", body)
	}
	if strings.Contains(string(body), "\ntools:") {
		t.Errorf("the tool allowlist rendered under `tools`, which Claude Code ignores:\n%s", body)
	}
}

// A skill that dispatches to a subagent needs that subagent rendered beside it.
// The pairing is invisible from either file alone, which is how it went missing.
func TestASkillsSubagentIsRenderedAlongsideIt(t *testing.T) {
	apply := renderDefaultStack(t)

	skill, err := os.ReadFile(filepath.Join(apply, ".claude/skills/wrap-session/SKILL.md"))
	if err != nil {
		t.Fatalf("wrap-session did not reach the consumer: %v", err)
	}
	if !strings.Contains(string(skill), "wrap-session-reviewer") {
		t.Skip("wrap-session no longer dispatches to a subagent")
	}
	if _, err := os.Stat(filepath.Join(apply, ".claude/agents/wrap-session-reviewer/AGENT.md")); err != nil {
		t.Errorf("wrap-session dispatches to wrap-session-reviewer, which never rendered: %v", err)
	}
}

// Both hooks have to arrive under the event they declare: an adapter that
// silently skipped an event would leave the store's digest and its backlog
// report absent with nothing failing.
func TestTheMemoryHooksReachTheConsumerUnderTheirEvents(t *testing.T) {
	apply := renderDefaultStack(t)

	settings, err := os.ReadFile(filepath.Join(apply, ".claude/settings.json"))
	if err != nil {
		t.Fatalf("settings did not reach the consumer: %v", err)
	}
	for _, event := range []string{"SessionStart", "SessionEnd"} {
		if !strings.Contains(string(settings), event) {
			t.Errorf("settings.json carries no %s hook:\n%s", event, settings)
		}
	}
	if !strings.Contains(string(settings), "agtk memory candidates") {
		t.Error("the session-end hook did not reach settings.json")
	}
}

// A pre-approved permission for a command no agent can run is dead config, and
// one the agent needs but nobody approved is a prompt on every delegation.
// Both are only visible once the settings are actually rendered.
func TestPreApprovedPermissionsNameCommandsThatExist(t *testing.T) {
	apply := renderDefaultStack(t)

	settings, err := os.ReadFile(filepath.Join(apply, ".claude/settings.json"))
	if err != nil {
		t.Fatalf("settings did not reach the consumer: %v", err)
	}
	for _, allowed := range []string{
		"agtk memory stats",
		"agtk memory show",
		"agtk memory candidates",
	} {
		if !strings.Contains(string(settings), allowed) {
			t.Errorf("settings.json pre-approves no %q", allowed)
		}
	}

	// The store-path grants come from the claude adapter rather than from the
	// settings definition, built from the entry manifest's memory.root or,
	// unset, from memory.DefaultRoot. This pins the unset case, which is what
	// every consumer that never configured memory renders. A grant naming a
	// root agtk does not use is a prompt on every delegation, and the render
	// is the only place the two can be compared.
	for _, allowed := range []string{
		"Read(**/" + memory.DefaultRoot + "/INDEX.md)",
		"Write(**/" + memory.DefaultRoot + "/candidates/**)",
	} {
		if !strings.Contains(string(settings), allowed) {
			t.Errorf("settings.json pre-approves no %q, so the explorer prompts for the default store:\n%s", allowed, settings)
		}
	}

	// Curation spends money and rewrites notes/, so it stays a prompt the user
	// answers deliberately rather than a grant they never saw.
	if strings.Contains(string(settings), "agtk memory curate") {
		t.Error("settings.json pre-approves `agtk memory curate`, which must stay a deliberate prompt")
	}
}

// renderDefaultStack renders the repo's own definitions/ and stacks/default.yaml
// into a throwaway consumer, and returns that consumer's directory.
//
// The stack's one remote entry is dropped: it would put a network fetch on the
// path of a test whose subject is entirely local, and the definitions under
// test are the ones in this repo.
func renderDefaultStack(t *testing.T) string {
	t.Helper()

	repo := repoRoot(t)
	source := t.TempDir()
	for _, dir := range []string{"definitions", "stacks"} {
		if err := copyTree(filepath.Join(repo, dir), filepath.Join(source, dir)); err != nil {
			t.Fatalf("copy %s: %v", dir, err)
		}
	}
	dropRemoteEntries(t, filepath.Join(source, "stacks", "default.yaml"))

	apply := t.TempDir()
	cache := t.TempDir()
	_, stderr, err := runCLI(t, apply, "--source", source, "--stack", "default", "sync", "--cache", cache)
	if err != nil {
		t.Fatalf("sync --source: %v\nstderr:\n%s", err, stderr)
	}
	return apply
}

// dropRemoteEntries removes the stack's URL-bearing entries in place.
func dropRemoteEntries(t *testing.T, stack string) {
	t.Helper()

	raw, err := os.ReadFile(stack)
	if err != nil {
		t.Fatalf("read stack: %v", err)
	}
	var kept []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "://") || strings.Contains(line, ".git/") {
			continue
		}
		kept = append(kept, line)
	}
	if err := os.WriteFile(stack, []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		t.Fatalf("write stack: %v", err)
	}
}

// repoRoot walks up from the test's own directory to the repo root, which it
// recognises by .git — a directory in a clone and a file in a worktree.
//
// Callers join both catalog paths (definitions/, stacks/) and source paths
// (source/toolkit/internal/...) onto what this returns, so it has to be the
// repo root and not the module root: go.mod lives under source/toolkit, and
// keying on it resolves source/toolkit/source/toolkit/internal.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no .git above the test directory")
		}
		dir = parent
	}
}

// memory-seed dispatches memory-explorer and hands off to memory-curate. Both
// pairings are invisible from the command's own file, which is exactly the
// shape that left wrap-session dispatching to an unrendered agent.
func TestMemorySeedsDispatchTargetsAreRenderedAlongsideIt(t *testing.T) {
	apply := renderDefaultStack(t)

	seed, err := os.ReadFile(filepath.Join(apply, ".claude/commands/memory-seed.md"))
	if err != nil {
		t.Fatalf("memory-seed did not reach the consumer: %v", err)
	}
	if !strings.Contains(string(seed), "memory-explorer") {
		t.Fatalf("memory-seed does not dispatch the explorer:\n%s", seed)
	}
	if _, err := os.Stat(filepath.Join(apply, ".claude/agents/memory-explorer/AGENT.md")); err != nil {
		t.Errorf("memory-seed dispatches memory-explorer, which never rendered: %v", err)
	}
	if !strings.Contains(string(seed), "memory-curate") {
		t.Error("memory-seed does not hand off to the curator, so a seeded backlog is never promoted")
	}
	if _, err := os.Stat(filepath.Join(apply, ".claude/commands/memory-curate.md")); err != nil {
		t.Errorf("memory-seed hands off to /memory-curate, which never rendered: %v", err)
	}

	// The seeding pass stages candidates and stops. A command that curated as
	// well would give notes/ a second writer, which is the rule ADR 0003 and
	// the curator's whole design rest on.
	if !strings.Contains(string(seed), "Do not curate") {
		t.Error("memory-seed does not tell the seeding session to leave curation alone")
	}

	// The explorer's own bar selects nothing in a cold sweep: it reads
	// everything fresh, so everything cost it exploration. A seed pass that
	// forgets to replace the bar stages the file locations the store exists
	// not to hold.
	if !strings.Contains(string(seed), "would a competent engineer reading this code get this wrong") {
		t.Errorf("memory-seed does not replace the cost bar for a cold sweep:\n%s", seed)
	}
}
