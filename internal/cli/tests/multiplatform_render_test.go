package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRender_MultiPlatform_EveryCategory renders a stack naming one of
// every category into both platforms at once, and checks each category
// against the destination that platform actually reads it from. The
// fixture includes a skill and a command sharing a name, which collide
// only on Codex — the two are separate destinations on Claude.
func TestRender_MultiPlatform_EveryCategory(t *testing.T) {
	work, stdout := renderAllCategories(t, "render", "--cache")

	// Claude: a directory per category under .claude, CLAUDE.md for
	// instructions, and the two JSON files for the rest.
	for _, rel := range []string{
		".claude/skills/review/SKILL.md",
		".claude/agents/helper/AGENT.md",
		".claude/commands/review.md",
		".claude/commands/deploy.md",
		".claude/rules/style.md",
		".claude/settings.json",
		".mcp.json",
		"CLAUDE.md",
	} {
		if _, err := os.Stat(filepath.Join(work, rel)); err != nil {
			t.Errorf("claude: %s not rendered: %v", rel, err)
		}
	}

	// Codex: two non-nesting roots, AGENTS.md, and one config file.
	for _, rel := range []string{
		".agents/skills/review/SKILL.md",
		".agents/skills/deploy/SKILL.md",
		".agents/rules/style.md",
		".codex/agents/helper.toml",
		".codex/config.toml",
		"AGENTS.md",
	} {
		if _, err := os.Stat(filepath.Join(work, rel)); err != nil {
			t.Errorf("codex: %s not rendered: %v", rel, err)
		}
	}

	// The colliding command loses to the authored skill on Codex only.
	codexReview := readFile(t, filepath.Join(work, ".agents/skills/review/SKILL.md"))
	if !strings.Contains(codexReview, "Authored skill body") {
		t.Errorf("codex: the command overwrote the authored skill:\n%s", codexReview)
	}
	if !strings.Contains(stdout, `command "review" not rendered`) {
		t.Errorf("codex: dropped command not reported: %q", stdout)
	}
	claudeReview := readFile(t, filepath.Join(work, ".claude/commands/review.md"))
	if !strings.Contains(claudeReview, "Colliding command body") {
		t.Errorf("claude: the command should still render as a command:\n%s", claudeReview)
	}

	// The non-colliding command carries the invocation restriction that
	// Codex's frontmatter has no flag for.
	deploy := readFile(t, filepath.Join(work, ".agents/skills/deploy/SKILL.md"))
	if !strings.Contains(deploy, "do not invoke it on your own") {
		t.Errorf("codex: converted command missing its invocation notice:\n%s", deploy)
	}

	// AGENTS.md and CLAUDE.md both carry the instruction, independently —
	// neither adapter reads the other's file.
	for _, rel := range []string{"AGENTS.md", "CLAUDE.md"} {
		if got := readFile(t, filepath.Join(work, rel)); !strings.Contains(got, "Write it the way the neighbours wrote it.") {
			t.Errorf("%s missing the instruction body:\n%s", rel, got)
		}
	}
	if got := readFile(t, filepath.Join(work, "AGENTS.md")); !strings.Contains(got, "[style](.agents/rules/style.md)") {
		t.Errorf("AGENTS.md missing the rule index Codex needs to find the rule:\n%s", got)
	}

	// Codex folds mcp, settings and hooks into one file where Claude uses
	// two, and turns the feature flag on so the hook is not inert.
	cfg := readFile(t, filepath.Join(work, ".codex/config.toml"))
	for _, want := range []string{
		"[mcp_servers.ctx]",
		"[[hooks.PreToolUse]]",
		"hooks = true",
		"model = 'gpt-5-codex'",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config.toml missing %q:\n%s", want, cfg)
		}
	}

}

// TestRender_MultiPlatform_DryRunWritesNeitherLayout: --dry-run reports
// both platforms' targets and creates neither platform's roots.
func TestRender_MultiPlatform_DryRunWritesNeitherLayout(t *testing.T) {
	work, stdout := renderAllCategories(t, "render", "--dry-run", "--cache")

	if !strings.Contains(stdout, "would write") {
		t.Errorf("dry-run stdout missing 'would write': %q", stdout)
	}
	for _, rel := range []string{".claude", ".agents", ".codex", "AGENTS.md", "CLAUDE.md"} {
		if _, err := os.Stat(filepath.Join(work, rel)); !os.IsNotExist(err) {
			t.Errorf("dry-run created %s (err=%v)", rel, err)
		}
	}
}

// TestRender_MultiPlatform_ForceOverwritesBothLayouts: an untracked file
// under either platform's root refuses the render, and --force takes it
// over. Both adapters get this from the same shared machinery, so the
// refusal reads the same on either side.
func TestRender_MultiPlatform_ForceOverwritesBothLayouts(t *testing.T) {
	work, cache := allCategoriesWorkdir(t)

	claudeSquatter := filepath.Join(work, ".claude/rules/style.md")
	codexSquatter := filepath.Join(work, ".agents/rules/style.md")
	for _, p := range []string{claudeSquatter, codexSquatter} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, p, "hand-authored\n")
	}

	_, _, err := runCLI(t, work, "render", "--cache", cache)
	if err == nil {
		t.Fatal("expected a collision refusal for untracked files")
	}
	for _, p := range []string{claudeSquatter, codexSquatter} {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("refusal should name %s: %v", p, err)
		}
		if got := readFile(t, p); got != "hand-authored\n" {
			t.Errorf("refused render still wrote %s: %q", p, got)
		}
	}

	if _, _, err := runCLI(t, work, "render", "--cache", cache, "--force"); err != nil {
		t.Fatalf("render --force: %v", err)
	}
	for _, p := range []string{claudeSquatter, codexSquatter} {
		if got := readFile(t, p); !strings.Contains(got, "Style rule body") {
			t.Errorf("--force did not take over %s: %q", p, got)
		}
	}
}

// allCategoriesWorkdir stages the every-category fixture as a stack
// opted into both platforms.
func allCategoriesWorkdir(t *testing.T) (work, cache string) {
	t.Helper()
	url, sha := fixtureRepoFromDir(t, "testdata/allcategories")
	work = t.TempDir()
	cache = t.TempDir()

	body := "extends:\n  - " + url + "/stacks/default.yaml@main\n" +
		"platforms:\n  - claude\n  - codex\n"
	writeFile(t, filepath.Join(work, ".agentic-toolkit.yaml"), body)
	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), url, "main", sha)
	return work, cache
}

func renderAllCategories(t *testing.T, args ...string) (work, stdout string) {
	t.Helper()
	work, cache := allCategoriesWorkdir(t)
	stdout, _, err := runCLI(t, work, append(args, cache)...)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return work, stdout
}
