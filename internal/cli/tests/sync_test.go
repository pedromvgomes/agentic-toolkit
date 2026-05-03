package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSync_FromCleanState_LocksFetchesRenders runs `agtk sync` in a
// fresh workdir with no lockfile. Sync should resolve, write the
// lockfile, hydrate the cache, and render the .claude/ tree in one
// command — output equivalent to lock + render run separately.
func TestSync_FromCleanState_LocksFetchesRenders(t *testing.T) {
	url, _ := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryStack(t, work, url, "main")

	if _, _, err := runCLI(t, work, "sync", "--cache", cache); err != nil {
		t.Fatalf("sync: %v", err)
	}

	lockPath := filepath.Join(work, ".agentic-toolkit.lock.yaml")
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("lockfile not written: %v", err)
	}
	skill := filepath.Join(work, ".claude/skills/foo/SKILL.md")
	if _, err := os.Stat(skill); err != nil {
		t.Errorf("skill not rendered: %v", err)
	}
}

// TestSync_FreshLockfile_NoRelock runs sync twice. The second run should
// see an up-to-date lockfile and skip the network re-lock.
func TestSync_FreshLockfile_NoRelock(t *testing.T) {
	url, _ := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryStack(t, work, url, "main")
	if _, _, err := runCLI(t, work, "sync", "--cache", cache); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	stdout, _, err := runCLI(t, work, "sync", "--cache", cache)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if strings.Contains(stdout, "locking against the network") {
		t.Errorf("second sync should skip relock, got stdout: %q", stdout)
	}
}

// TestSync_StaleConfig_Relocks checks that touching the config triggers
// a re-lock on the next sync.
func TestSync_StaleConfig_Relocks(t *testing.T) {
	url, _ := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryStack(t, work, url, "main")
	if _, _, err := runCLI(t, work, "sync", "--cache", cache); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// Bump config mtime so it appears newer than the lockfile.
	configPath := filepath.Join(work, ".agentic-toolkit.yaml")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(configPath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	stdout, _, err := runCLI(t, work, "sync", "--cache", cache)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if !strings.Contains(stdout, "locking against the network") {
		t.Errorf("stale config should trigger relock, got: %q", stdout)
	}
}
