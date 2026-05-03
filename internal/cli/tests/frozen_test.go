package tests

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestLockFrozen_CleanLockfile_Succeeds: an existing lockfile that
// matches what `agtk lock` would resolve passes --frozen.
func TestLockFrozen_CleanLockfile_Succeeds(t *testing.T) {
	url, sha := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryStack(t, work, url, "main")
	if _, _, err := runCLI(t, work, "lock", "--cache", cache); err != nil {
		t.Fatalf("seed lock: %v", err)
	}

	stdout, _, err := runCLI(t, work, "lock", "--cache", cache, "--frozen")
	if err != nil {
		t.Fatalf("lock --frozen on clean: %v", err)
	}
	if !strings.Contains(stdout, "up to date") {
		t.Errorf("expected 'up to date' on clean: %q", stdout)
	}
	_ = sha
}

// TestLockFrozen_DriftedLockfile_Errors: a lockfile that no longer
// matches what `agtk lock` would resolve fails --frozen.
func TestLockFrozen_DriftedLockfile_Errors(t *testing.T) {
	url, _ := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryStack(t, work, url, "main")
	// Hand-write a lockfile with a wrong SHA.
	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"),
		url, "main", "0000000000000000000000000000000000000000")

	_, _, err := runCLI(t, work, "lock", "--cache", cache, "--frozen")
	if err == nil {
		t.Fatal("expected error on lockfile drift")
	}
	if !strings.Contains(err.Error(), "would change") {
		t.Errorf("error should explain that the lockfile would change: %v", err)
	}
}

// TestLockFrozen_MissingLockfile_Errors: a missing lockfile fails
// --frozen with a specific message.
func TestLockFrozen_MissingLockfile_Errors(t *testing.T) {
	url, _ := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryStack(t, work, url, "main")

	_, _, err := runCLI(t, work, "lock", "--cache", cache, "--frozen")
	if err == nil {
		t.Fatal("expected error when lockfile is missing")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error should mention the missing lockfile: %v", err)
	}
}

// TestLockJSON_WroteAction emits a stable schema with action=wrote.
func TestLockJSON_WroteAction(t *testing.T) {
	url, _ := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryStack(t, work, url, "main")

	stdout, _, err := runCLI(t, work, "lock", "--cache", cache, "--json")
	if err != nil {
		t.Fatalf("lock --json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("parse json: %v\n%s", err, stdout)
	}
	if got["version"] != float64(1) {
		t.Errorf("version = %v, want 1", got["version"])
	}
	if got["action"] != "wrote" {
		t.Errorf("action = %v, want wrote", got["action"])
	}
	if _, ok := got["lockfile"].(map[string]any); !ok {
		t.Errorf("lockfile field missing or wrong type: %v", got["lockfile"])
	}
}
