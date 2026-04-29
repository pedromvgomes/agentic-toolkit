package sourcestore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Cache is the on-disk source cache. Layout:
//
//	<root>/sources/<sha256(repoURL)>/<sha>/
//
// Worktrees are immutable per SHA. Two distinct repoURLs that map to the
// same SHA do not share storage — different paths under different URL
// digests. This is intentional: it preserves provenance and avoids
// having to canonicalize divergent URL spellings (https vs ssh, with or
// without `.git`).
type Cache struct {
	root string
}

// NewCache returns a Cache rooted at the given directory. The directory
// is created on demand by hydration calls; NewCache itself does not
// touch the filesystem.
func NewCache(root string) *Cache { return &Cache{root: root} }

// DefaultCache returns a Cache rooted at $XDG_CACHE_HOME/agentic-toolkit
// (or ~/.cache/agentic-toolkit if XDG_CACHE_HOME is unset).
func DefaultCache() (*Cache, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("locate home for default cache: %w", err)
		}
		base = filepath.Join(home, ".cache")
	}
	return NewCache(filepath.Join(base, "agentic-toolkit")), nil
}

// Root returns the cache root directory.
func (c *Cache) Root() string { return c.root }

// shaDir returns the on-disk path for a (repoURL, sha) worktree.
func (c *Cache) shaDir(repoURL, sha string) string {
	h := sha256.Sum256([]byte(repoURL))
	return filepath.Join(c.root, "sources", hex.EncodeToString(h[:]), sha)
}

// has reports whether a worktree for (repoURL, sha) already exists.
func (c *Cache) has(repoURL, sha string) bool {
	info, err := os.Stat(c.shaDir(repoURL, sha))
	return err == nil && info.IsDir()
}

// open returns an fs.FS over the cached worktree for (repoURL, sha),
// optionally rooted at subPath inside that worktree.
func (c *Cache) open(repoURL, sha, subPath string) (fs.FS, error) {
	dir := c.shaDir(repoURL, sha)
	root := os.DirFS(dir)
	if subPath == "" {
		return root, nil
	}
	sub, err := fs.Sub(root, subPath)
	if err != nil {
		return nil, fmt.Errorf("subpath %q in cached worktree %s: %w", subPath, dir, err)
	}
	return sub, nil
}
