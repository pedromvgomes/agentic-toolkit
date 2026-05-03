package tests

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/lockfile"
	"github.com/pedromvgomes/agentic-toolkit/internal/sourceref"
	"github.com/pedromvgomes/agentic-toolkit/internal/sourcestore"
)

func TestFrozenProvider_ServesPinnedSource(t *testing.T) {
	url, sha := fixtureRepo(t, map[string]string{
		"definitions/skills/foo/SKILL.md": "skill content",
	})
	lock := &lockfile.Lockfile{
		Version: lockfile.Version,
		Sources: []lockfile.ResolvedSource{{URL: url, Ref: "main", SHA: sha}},
	}
	provider := sourcestore.NewFrozenProvider(sourcestore.NewCache(t.TempDir()), lock)

	fsys, rr, err := provider.Provide(sourceref.Source{URL: url, Ref: "main"})
	if err != nil {
		t.Fatalf("Provide: %v", err)
	}
	if rr.SHA != sha || rr.Ref != "main" {
		t.Errorf("rr = %+v, want sha=%s ref=main", rr, sha)
	}
	if got := readFSFile(t, fsys, "definitions/skills/foo/SKILL.md"); got != "skill content" {
		t.Errorf("file content: %q", got)
	}
}

func TestFrozenProvider_EmptyConfigRef_FallsBackToUniquePinForURL(t *testing.T) {
	url, sha := fixtureRepo(t, map[string]string{"README.md": "x"})
	lock := &lockfile.Lockfile{
		Version: lockfile.Version,
		Sources: []lockfile.ResolvedSource{{URL: url, Ref: "main", SHA: sha}},
	}
	provider := sourcestore.NewFrozenProvider(sourcestore.NewCache(t.TempDir()), lock)

	// User config has empty ref; lockfile carries the resolved ref name.
	_, rr, err := provider.Provide(sourceref.Source{URL: url})
	if err != nil {
		t.Fatalf("Provide with empty ref: %v", err)
	}
	if rr.Ref != "main" || rr.SHA != sha {
		t.Errorf("rr = %+v, want main/%s", rr, sha)
	}
}

func TestFrozenProvider_UnpinnedSource_Errors(t *testing.T) {
	url, sha := fixtureRepo(t, map[string]string{"README.md": "x"})
	lock := &lockfile.Lockfile{
		Version: lockfile.Version,
		Sources: []lockfile.ResolvedSource{{URL: url, Ref: "main", SHA: sha}},
	}
	provider := sourcestore.NewFrozenProvider(sourcestore.NewCache(t.TempDir()), lock)

	_, _, err := provider.Provide(sourceref.Source{URL: "github.com/other/repo", Ref: "main"})
	if !errors.Is(err, sourcestore.ErrPinNotFound) {
		t.Errorf("expected ErrPinNotFound, got %v", err)
	}
}

func TestFrozenProvider_DirectRefURL_RootedAtBundleDir(t *testing.T) {
	url, sha := fixtureRepo(t, map[string]string{
		"skills/foo/SKILL.md": "the foo skill",
	})
	directURL := url + "/skills/foo"
	lock := &lockfile.Lockfile{
		Version: lockfile.Version,
		Sources: []lockfile.ResolvedSource{{URL: directURL, Ref: "main", SHA: sha}},
	}
	provider := sourcestore.NewFrozenProvider(sourcestore.NewCache(t.TempDir()), lock)

	fsys, _, err := provider.Provide(sourceref.Source{URL: directURL, Ref: "main"})
	if err != nil {
		t.Fatalf("Provide: %v", err)
	}
	if got := readFSFile(t, fsys, "SKILL.md"); got != "the foo skill" {
		t.Errorf("SKILL.md content: %q", got)
	}
}

func TestFrozenProvider_SHAMismatch_HardFails(t *testing.T) {
	url, _ := fixtureRepo(t, map[string]string{"README.md": "x"})
	// Pin a SHA that doesn't match what main currently points at.
	lock := &lockfile.Lockfile{
		Version: lockfile.Version,
		Sources: []lockfile.ResolvedSource{{
			URL: url, Ref: "main",
			SHA: "0000000000000000000000000000000000000000",
		}},
	}
	provider := sourcestore.NewFrozenProvider(sourcestore.NewCache(t.TempDir()), lock)

	_, _, err := provider.Provide(sourceref.Source{URL: url, Ref: "main"})
	if !errors.Is(err, sourcestore.ErrSHAMismatch) {
		t.Errorf("expected ErrSHAMismatch, got %v", err)
	}
}

func TestFrozenProvider_Hydrate_FetchesAllPinsAndIsIdempotent(t *testing.T) {
	url, sha := fixtureRepo(t, map[string]string{"README.md": "x"})
	cacheRoot := t.TempDir()
	lock := &lockfile.Lockfile{
		Version: lockfile.Version,
		Sources: []lockfile.ResolvedSource{{URL: url, Ref: "main", SHA: sha}},
	}
	provider := sourcestore.NewFrozenProvider(sourcestore.NewCache(cacheRoot), lock)

	if err := provider.Hydrate(); err != nil {
		t.Fatalf("first Hydrate: %v", err)
	}
	// Second hydrate should be a no-op (cache hit on every pin).
	bare := strings.TrimPrefix(url, "file://")
	moved := bare + ".gone"
	if err := os.Rename(bare, moved); err != nil {
		t.Fatalf("rename: %v", err)
	}
	defer os.Rename(moved, bare)
	if err := provider.Hydrate(); err != nil {
		t.Errorf("idempotent Hydrate failed after remote vanished: %v", err)
	}
}
