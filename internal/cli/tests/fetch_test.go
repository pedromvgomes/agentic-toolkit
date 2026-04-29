package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetch_HydratesCacheFromLockfile(t *testing.T) {
	url, sha := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), url, "main", sha)

	stdout, _, err := runCLI(t, work, "fetch", "--cache", cache)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(stdout, "fetched 1 source") {
		t.Errorf("stdout should report count: %q", stdout)
	}
	expected := filepath.Join(cache, "sources", urlDigest(url), sha)
	if info, err := os.Stat(expected); err != nil || !info.IsDir() {
		t.Errorf("expected cache dir %s to exist after fetch", expected)
	}
}

func TestFetch_MissingLockfile_Errors(t *testing.T) {
	work := t.TempDir()
	cache := t.TempDir()

	_, _, err := runCLI(t, work, "fetch", "--cache", cache)
	if err == nil {
		t.Fatal("expected error when lockfile is missing")
	}
	if !strings.Contains(err.Error(), "agtk lock") {
		t.Errorf("error should hint at running `agtk lock`: %v", err)
	}
}

func TestFetch_Idempotent_OnSecondRun(t *testing.T) {
	url, sha := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), url, "main", sha)

	if _, _, err := runCLI(t, work, "fetch", "--cache", cache); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	// Move the bare repo aside; a second fetch must succeed against the cache only.
	bare := strings.TrimPrefix(url, "file://")
	moved := bare + ".gone"
	if err := os.Rename(bare, moved); err != nil {
		t.Fatalf("rename: %v", err)
	}
	defer os.Rename(moved, bare)

	if _, _, err := runCLI(t, work, "fetch", "--cache", cache); err != nil {
		t.Errorf("idempotent fetch failed after remote vanished: %v", err)
	}
}

// writeLockfile writes a hand-crafted single-entry lockfile. We avoid
// invoking the resolver here so fetch_test.go is independent of lock_test.go.
func writeLockfile(t *testing.T, path, url, ref, sha string) {
	t.Helper()
	body := "version: 1\nsources:\n  - url: " + url + "\n    ref: " + ref + "\n    sha: " + sha + "\n"
	writeFile(t, path, body)
}

// urlDigest mirrors the cache layout: sha256(url) hex.
func urlDigest(url string) string {
	h := sha256.Sum256([]byte(url))
	return hex.EncodeToString(h[:])
}
