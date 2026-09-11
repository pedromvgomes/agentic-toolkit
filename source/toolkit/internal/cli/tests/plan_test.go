package tests

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPlan_PrintsSourcesAndDefinitions(t *testing.T) {
	url, sha := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryStack(t, work, url, "main")
	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), url, "main", sha)

	stdout, _, err := runCLI(t, work, "plan", "--cache", cache)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !strings.Contains(stdout, "sources:") {
		t.Errorf("stdout should have a sources section: %q", stdout)
	}
	if !strings.Contains(stdout, url) {
		t.Errorf("stdout should mention the source URL: %q", stdout)
	}
	if !strings.Contains(stdout, "skills:") || !strings.Contains(stdout, "foo") {
		t.Errorf("stdout should list the foo skill: %q", stdout)
	}
}

func TestPlan_MissingLockfile_Errors(t *testing.T) {
	url, _ := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryStack(t, work, url, "main")

	_, _, err := runCLI(t, work, "plan", "--cache", cache)
	if err == nil {
		t.Fatal("expected error when lockfile is missing")
	}
	if !strings.Contains(err.Error(), "agtk lock") {
		t.Errorf("error should hint at running `agtk lock`: %v", err)
	}
}

func TestPlan_DriftedSource_NotInLockfile_Errors(t *testing.T) {
	url, sha := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	// Config points at the fixture, lockfile pins a different URL — frozen
	// provider should refuse the unpinned source.
	writeEntryStack(t, work, url, "main")
	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"),
		"github.com/somewhere/else", "main", sha)

	_, _, err := runCLI(t, work, "plan", "--cache", cache)
	if err == nil {
		t.Fatal("expected error when config source is not in lockfile")
	}
	if !strings.Contains(err.Error(), "not pinned") {
		t.Errorf("error should mention pin mismatch: %v", err)
	}
}
