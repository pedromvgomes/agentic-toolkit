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

// An edited manifest is what sends sync back to the network, and it does so
// however the edit left the file's timestamps.
func TestSync_EditedConfig_Relocks(t *testing.T) {
	url, _ := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryStack(t, work, url, "main")
	if _, _, err := runCLI(t, work, "sync", "--cache", cache); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	configPath := filepath.Join(work, ".agentic-toolkit.yaml")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, append(raw, "\n# an edit\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	// Backdated, so nothing about the edit is visible in the timestamps.
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(configPath, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	stdout, _, err := runCLI(t, work, "sync", "--cache", cache)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if !strings.Contains(stdout, "locking against the network") {
		t.Errorf("an edited manifest did not trigger a relock, got: %q", stdout)
	}
}

// A newer timestamp over identical content is not a reason to reach the
// network. Re-locking is the only branch in sync that does, so an offline
// runner fails here on a repo whose committed lockfile was usable.
func TestSync_TouchedConfig_DoesNotRelock(t *testing.T) {
	url, _ := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryStack(t, work, url, "main")
	if _, _, err := runCLI(t, work, "sync", "--cache", cache); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	configPath := filepath.Join(work, ".agentic-toolkit.yaml")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(configPath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	stdout, _, err := runCLI(t, work, "sync", "--cache", cache)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if strings.Contains(stdout, "locking against the network") {
		t.Errorf("an untouched manifest with a bumped mtime went to the network, got: %q", stdout)
	}
}
