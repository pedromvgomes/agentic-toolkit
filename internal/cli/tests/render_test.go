package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRender_WritesScopeRoot(t *testing.T) {
	url, sha := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryStack(t, work, url, "main")
	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), url, "main", sha)

	_, _, err := runCLI(t, work, "render", "--cache", cache)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	skill := filepath.Join(work, ".claude/skills/foo/SKILL.md")
	if _, err := os.Stat(skill); err != nil {
		t.Errorf("skill not rendered at %s: %v", skill, err)
	}
	manifest := filepath.Join(work, ".claude/.agtk-manifest.json")
	if _, err := os.Stat(manifest); err != nil {
		t.Errorf("manifest not written at %s: %v", manifest, err)
	}
}

func TestRender_DryRunNoWrites(t *testing.T) {
	url, sha := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryStack(t, work, url, "main")
	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), url, "main", sha)

	stdout, _, err := runCLI(t, work, "render", "--cache", cache, "--dry-run")
	if err != nil {
		t.Fatalf("render dry-run: %v", err)
	}
	if !strings.Contains(stdout, "would write") {
		t.Errorf("dry-run stdout missing 'would write': %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(work, ".claude")); !os.IsNotExist(err) {
		t.Errorf("dry-run created .claude/ (err=%v)", err)
	}
}

// TestRender_MultiPlatform_RendersBothLayouts: a stack opting into
// platforms: [claude, codex] renders both adapters' layouts from the
// same definitions in one `agtk render`.
func TestRender_MultiPlatform_RendersBothLayouts(t *testing.T) {
	url, sha := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	body := "extends:\n  - " + url + "/stacks/default.yaml@main\n" +
		"platforms:\n  - claude\n  - codex\n"
	writeFile(t, filepath.Join(work, ".agentic-toolkit.yaml"), body)
	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), url, "main", sha)

	_, _, err := runCLI(t, work, "render", "--cache", cache)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	claudeSkill := filepath.Join(work, ".claude/skills/foo/SKILL.md")
	if _, err := os.Stat(claudeSkill); err != nil {
		t.Errorf("claude skill not rendered at %s: %v", claudeSkill, err)
	}
	codexSkill := filepath.Join(work, ".agents/skills/foo/SKILL.md")
	if _, err := os.Stat(codexSkill); err != nil {
		t.Errorf("codex skill not rendered at %s: %v", codexSkill, err)
	}
}

// TestRender_UnknownAdapterPlatform_ErrorsAtRenderTime: a platform name
// valid at parse time (a real definitions.Platform value) but with no
// render adapter here is a render-time error, not a silent no-op — a
// different layer than the parse-time rejection of a genuinely unknown
// platform name (see internal/stack/tests).
func TestRender_UnknownAdapterPlatform_ErrorsAtRenderTime(t *testing.T) {
	url, sha := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	body := "extends:\n  - " + url + "/stacks/default.yaml@main\n" +
		"platforms:\n  - cursor\n"
	writeFile(t, filepath.Join(work, ".agentic-toolkit.yaml"), body)
	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), url, "main", sha)

	_, _, err := runCLI(t, work, "render", "--cache", cache)
	if err == nil {
		t.Fatal("expected error for a platform with no render adapter")
	}
	if !strings.Contains(err.Error(), `no adapter for platform "cursor"`) {
		t.Errorf("error should name the adapter-less platform: %v", err)
	}
}

func TestRender_InvalidScope_Errors(t *testing.T) {
	work := t.TempDir()
	writeFile(t, filepath.Join(work, ".agentic-toolkit.yaml"), "source: file:///nope\n")
	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), "file:///nope", "main", "deadbeef")

	_, _, err := runCLI(t, work, "render", "--scope", "global")
	if err == nil {
		t.Fatal("expected error for invalid scope")
	}
	if !strings.Contains(err.Error(), "invalid --scope") {
		t.Errorf("error should mention invalid scope: %v", err)
	}
}
