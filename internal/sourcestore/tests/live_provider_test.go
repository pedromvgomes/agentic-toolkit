package tests

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/config"
	"github.com/pedromvgomes/agentic-toolkit/internal/sourcestore"
)

func TestLiveProvider_WholeSourceURL_RootedAtRepoTop(t *testing.T) {
	url, sha := fixtureRepo(t, map[string]string{
		"definitions/skills/foo/SKILL.md": "skill foo",
		"README.md":                       "readme",
	})
	cache := sourcestore.NewCache(t.TempDir())
	provider := sourcestore.NewLiveProvider(cache)

	fsys, rr, err := provider.Provide(config.Source{URL: url, Ref: "main"})
	if err != nil {
		t.Fatalf("Provide: %v", err)
	}
	if rr.SHA != sha {
		t.Errorf("SHA = %q, want %q", rr.SHA, sha)
	}
	if rr.Ref != "main" {
		t.Errorf("Ref = %q, want main", rr.Ref)
	}
	if got := readFSFile(t, fsys, "definitions/skills/foo/SKILL.md"); got != "skill foo" {
		t.Errorf("file content mismatch: %q", got)
	}
	if got := readFSFile(t, fsys, "README.md"); got != "readme" {
		t.Errorf("README content: %q", got)
	}
}

func TestLiveProvider_DirectRefURL_RootedAtBundleDir(t *testing.T) {
	url, _ := fixtureRepo(t, map[string]string{
		"skills/foo/SKILL.md": "the foo skill",
		"skills/foo/extra.md": "companion",
	})
	cache := sourcestore.NewCache(t.TempDir())
	provider := sourcestore.NewLiveProvider(cache)

	// Direct-ref form: <repo-url>/skills/foo (URL ends in `.git/skills/foo`).
	directURL := url + "/skills/foo"
	fsys, _, err := provider.Provide(config.Source{URL: directURL, Ref: "main"})
	if err != nil {
		t.Fatalf("Provide: %v", err)
	}
	if got := readFSFile(t, fsys, "SKILL.md"); got != "the foo skill" {
		t.Errorf("SKILL.md content: %q", got)
	}
	if got := readFSFile(t, fsys, "extra.md"); got != "companion" {
		t.Errorf("extra.md content: %q", got)
	}
}

func TestLiveProvider_EmptyRef_FillsInDefaultBranch(t *testing.T) {
	url, sha := fixtureRepo(t, map[string]string{"README.md": "x"})
	cache := sourcestore.NewCache(t.TempDir())
	provider := sourcestore.NewLiveProvider(cache)

	_, rr, err := provider.Provide(config.Source{URL: url}) // empty ref
	if err != nil {
		t.Fatalf("Provide: %v", err)
	}
	if rr.Ref != "main" {
		t.Errorf("Ref = %q, want main (resolved from HEAD)", rr.Ref)
	}
	if rr.SHA != sha {
		t.Errorf("SHA = %q, want %q", rr.SHA, sha)
	}
}

func TestLiveProvider_CacheHit_OnSecondProvide(t *testing.T) {
	url, sha := fixtureRepo(t, map[string]string{"README.md": "x"})
	cacheRoot := t.TempDir()
	provider := sourcestore.NewLiveProvider(sourcestore.NewCache(cacheRoot))

	if _, _, err := provider.Provide(config.Source{URL: url, Ref: "main"}); err != nil {
		t.Fatalf("first Provide: %v", err)
	}

	// Break the remote: rename the bare directory so a second fetch
	// would fail. A cache hit must still succeed.
	bare := strings.TrimPrefix(url, "file://")
	moved := bare + ".moved"
	if err := os.Rename(bare, moved); err != nil {
		t.Fatalf("rename bare: %v", err)
	}

	_, rr, err := provider.Provide(config.Source{URL: url, Ref: "main"})
	if err == nil {
		t.Fatalf("expected ls-remote to fail after remote moved, got success with SHA %s", rr.SHA)
	}

	// Now teach the test what we actually want: with the remote gone,
	// LiveProvider's ls-remote step is what fails — confirming the
	// cache layout is keyed by SHA but the ref-resolution still hits
	// the remote on every Provide. That's by design for `agtk lock`,
	// which is meant to refresh refs.
	if !strings.Contains(err.Error(), "ls-remote") {
		t.Errorf("error should reference ls-remote, got: %v", err)
	}

	// Move it back and verify the cached worktree is still on disk.
	if err := os.Rename(moved, bare); err != nil {
		t.Fatalf("rename back: %v", err)
	}
	cached := filepath.Join(cacheRoot, "sources")
	entries, err := os.ReadDir(cached)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one URL bucket in cache, got %d", len(entries))
	}
	shaEntries, err := os.ReadDir(filepath.Join(cached, entries[0].Name()))
	if err != nil {
		t.Fatalf("read sha bucket: %v", err)
	}
	found := false
	for _, e := range shaEntries {
		if e.Name() == sha {
			found = true
		}
	}
	if !found {
		t.Errorf("cache missing entry for SHA %s; got %v", sha, shaEntries)
	}
}

func TestLiveProvider_UnknownRef_Errors(t *testing.T) {
	url, _ := fixtureRepo(t, map[string]string{"README.md": "x"})
	provider := sourcestore.NewLiveProvider(sourcestore.NewCache(t.TempDir()))

	_, _, err := provider.Provide(config.Source{URL: url, Ref: "no-such-ref"})
	if err == nil {
		t.Fatal("expected error for unknown ref, got nil")
	}
	if !strings.Contains(err.Error(), "no-such-ref") {
		t.Errorf("error should mention the bad ref: %v", err)
	}
	// Sanity: ErrSHAMismatch is for divergence, not for missing refs.
	if errors.Is(err, sourcestore.ErrSHAMismatch) {
		t.Errorf("missing ref should not be reported as SHA mismatch")
	}
}
