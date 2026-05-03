package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The bare-repo + worktree workflow: the user runs from the bare-repo
// root, points --config at a worktree's manifest. Lockfile lands next
// to the manifest (in the worktree); render output (when applicable)
// stays at the apply dir (the bare-repo root).
//
// `agtk lock` doesn't render, so this test focuses on the lockfile
// landing at the right place and the config being read from --config
// rather than from cwd.
func TestLock_ConfigFlag_LockfileLandsNextToConfig(t *testing.T) {
	url, _ := fixtureRepoFromDir(t, "testdata/primary")
	root := t.TempDir()
	apply := root            // simulate "bare repo root" — cwd
	worktree := filepath.Join(root, "main")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	writeEntryStack(t, worktree, url, "main")

	cache := t.TempDir()
	configPath := filepath.Join(worktree, ".agentic-toolkit.yaml")

	stdout, _, err := runCLI(t, apply, "--config", configPath, "lock", "--cache", cache)
	if err != nil {
		t.Fatalf("lock --config: %v", err)
	}

	// Lockfile must be in the worktree dir (next to the config), NOT
	// in the apply dir (bare-repo root).
	wantLock := filepath.Join(worktree, ".agentic-toolkit.lock.yaml")
	if !strings.Contains(stdout, wantLock) {
		t.Errorf("stdout should reference lockfile at %q, got:\n%s", wantLock, stdout)
	}
	if _, err := os.Stat(wantLock); err != nil {
		t.Errorf("expected lockfile at %q, got: %v", wantLock, err)
	}

	// Apply dir must NOT have its own lockfile.
	strayLock := filepath.Join(apply, ".agentic-toolkit.lock.yaml")
	if _, err := os.Stat(strayLock); !os.IsNotExist(err) {
		t.Errorf("apply dir should not have a lockfile, but %q exists (err=%v)", strayLock, err)
	}
}

// init --config writes the scaffold at the supplied path, creating
// parent directories as needed.
func TestInit_ConfigFlag_WritesAtPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "nested", "dir", "team-stack.yaml")

	stdout, _, err := runCLI(t, root, "--config", target, "init",
		"--extends", "github.com/foo/bar.git/stacks/default.yaml@main")
	if err != nil {
		t.Fatalf("init --config: %v", err)
	}
	if !strings.Contains(stdout, target) {
		t.Errorf("stdout should mention written path %q, got:\n%s", target, stdout)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	if !strings.Contains(string(body), "github.com/foo/bar.git/stacks/default.yaml@main") {
		t.Errorf("scaffold should contain the extends URL, got:\n%s", body)
	}
}
