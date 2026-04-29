package tests

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/pedromvgomes/agentic-toolkit/internal/lockfile"
)

func TestLock_ResolvesPrimaryAndWritesLockfile(t *testing.T) {
	url, sha := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeFile(t, filepath.Join(work, ".agentic-toolkit.yaml"),
		"source: "+url+"@main\npresets:\n  - default\n")

	stdout, _, err := runCLI(t, work, "lock", "--cache", cache)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	lockPath := filepath.Join(work, ".agentic-toolkit.lock.yaml")
	if !strings.Contains(stdout, lockPath) {
		t.Errorf("stdout should mention written lockfile path: %q", stdout)
	}

	var lf lockfile.Lockfile
	if err := yaml.Unmarshal([]byte(readFile(t, lockPath)), &lf); err != nil {
		t.Fatalf("unmarshal lockfile: %v", err)
	}
	if lf.Version != lockfile.Version {
		t.Errorf("Version = %d, want %d", lf.Version, lockfile.Version)
	}
	if len(lf.Sources) != 1 {
		t.Fatalf("Sources = %d, want 1: %+v", len(lf.Sources), lf.Sources)
	}
	got := lf.Sources[0]
	if got.URL != url || got.Ref != "main" || got.SHA != sha {
		t.Errorf("Sources[0] = %+v, want url=%s ref=main sha=%s", got, url, sha)
	}
}

func TestLock_MissingConfig_Errors(t *testing.T) {
	work := t.TempDir()
	cache := t.TempDir()

	_, _, err := runCLI(t, work, "lock", "--cache", cache)
	if err == nil {
		t.Fatal("expected error when config is missing")
	}
	if !strings.Contains(err.Error(), ".agentic-toolkit.yaml") {
		t.Errorf("error should reference the missing config: %v", err)
	}
}
