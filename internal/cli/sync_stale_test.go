package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Staleness is a question about content: does this lockfile describe this
// manifest? Answered from mtimes it has two silent failure directions — a
// checkout that happens to write the manifest last re-locks against the
// network on a repo whose committed lockfile was perfectly usable, and an
// mtime-preserving restore renders from a lockfile that no longer matches.
func TestLockIsStale_ComparesContentNotModTime(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	lockPath := filepath.Join(dir, "lock.yaml")
	cfg := []byte("version: 1\nsources: []\n")

	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	writeLockFor(t, lockPath, cfg)

	// The manifest is newer than the lockfile and unchanged. mtime calls
	// that stale; content does not.
	touch(t, cfgPath, time.Now().Add(time.Hour))

	stale, err := lockIsStale(cfgPath, lockPath)
	if err != nil {
		t.Fatalf("lockIsStale: %v", err)
	}
	if stale {
		t.Error("an unchanged manifest was called stale, forcing a needless network re-lock")
	}
}

func TestLockIsStale_EditedManifestIsStaleEvenWithAnOlderModTime(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	lockPath := filepath.Join(dir, "lock.yaml")

	if err := os.WriteFile(cfgPath, []byte("version: 1\nsources: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLockFor(t, lockPath, []byte("version: 1\nsources: []\n"))

	if err := os.WriteFile(cfgPath, []byte("version: 1\nsources: [changed]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// An mtime-preserving restore leaves the edited manifest looking older
	// than the lockfile it no longer matches.
	touch(t, cfgPath, time.Now().Add(-time.Hour))

	stale, err := lockIsStale(cfgPath, lockPath)
	if err != nil {
		t.Fatalf("lockIsStale: %v", err)
	}
	if !stale {
		t.Error("an edited manifest was called fresh; sync would render from a lockfile that does not match it")
	}
}

// A lockfile written before the digest existed records nothing to compare
// against. Treating that as fresh would carry the mtime-era ambiguity
// forward indefinitely; treating it as stale costs one re-lock and then
// converges.
func TestLockIsStale_ALockfileWithNoDigestIsStale(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	lockPath := filepath.Join(dir, "lock.yaml")

	if err := os.WriteFile(cfgPath, []byte("version: 1\nsources: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("version: 2\nsources: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stale, err := lockIsStale(cfgPath, lockPath)
	if err != nil {
		t.Fatalf("lockIsStale: %v", err)
	}
	if !stale {
		t.Error("a lockfile recording no manifest digest was called fresh")
	}
}

func TestLockIsStale_MissingLockfileIsStale(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, err := lockIsStale(cfgPath, filepath.Join(dir, "absent.yaml"))
	if err != nil {
		t.Fatalf("lockIsStale: %v", err)
	}
	if !stale {
		t.Error("a missing lockfile is not stale")
	}
}

func writeLockFor(t *testing.T, path string, config []byte) {
	t.Helper()
	body := "version: 2\nsources: []\nconfig_digest: " + lockfileConfigDigest(config) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func touch(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}
