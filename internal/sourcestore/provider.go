package sourcestore

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/pedromvgomes/agentic-toolkit/internal/config"
	"github.com/pedromvgomes/agentic-toolkit/internal/lockfile"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// LiveProvider implements resolver.SourceProvider against a live remote.
// Each Provide call resolves the ref to a SHA via `git ls-remote`, then
// hydrates the cache for that SHA if absent. It is the provider used by
// `agtk lock`, where the goal is to write a fresh lockfile.
type LiveProvider struct {
	cache *Cache
}

// NewLiveProvider returns a provider that resolves refs against the
// network and writes into cache.
func NewLiveProvider(cache *Cache) *LiveProvider {
	return &LiveProvider{cache: cache}
}

// Provide resolves s and returns an fs.FS rooted appropriately for the
// URL shape (whole-source vs direct-ref). The returned ResolvedRef
// always carries a non-empty Ref — empty input refs are filled in with
// the remote's default branch name.
func (p *LiveProvider) Provide(s config.Source) (fs.FS, resolver.ResolvedRef, error) {
	repoURL, subPath := splitURL(s.URL)
	sha, resolvedRef, err := gitResolveRef(repoURL, s.Ref)
	if err != nil {
		return nil, resolver.ResolvedRef{}, fmt.Errorf("ls-remote %s: %w", repoURL, err)
	}
	if !p.cache.has(repoURL, sha) {
		if err := gitFetch(repoURL, resolvedRef, sha, p.cache.shaDir(repoURL, sha)); err != nil {
			return nil, resolver.ResolvedRef{}, err
		}
	}
	fsys, err := p.cache.open(repoURL, sha, subPath)
	if err != nil {
		return nil, resolver.ResolvedRef{}, err
	}
	return fsys, resolver.ResolvedRef{Ref: resolvedRef, SHA: sha}, nil
}

// FrozenProvider implements resolver.SourceProvider against a lockfile.
// It refuses to resolve any source whose (URL, Ref) pair is not pinned,
// and on cache miss it fetches the pinned SHA — never a fresh ref. Used
// by `agtk plan` (no network unless cache miss) and `agtk fetch`.
//
// Source matching is exact-key first; when an incoming Source has an
// empty Ref, the provider falls back to the unique pin under the same
// URL. This bridges the gap between user-written empty refs in the
// config and the resolved branch name recorded in the lockfile.
type FrozenProvider struct {
	cache *Cache
	byKey map[srcKey]lockfile.ResolvedSource
	byURL map[string][]lockfile.ResolvedSource
}

type srcKey struct {
	URL string
	Ref string
}

// NewFrozenProvider builds a provider over the given lockfile contents.
func NewFrozenProvider(cache *Cache, lock *lockfile.Lockfile) *FrozenProvider {
	byKey := make(map[srcKey]lockfile.ResolvedSource, len(lock.Sources))
	byURL := make(map[string][]lockfile.ResolvedSource, len(lock.Sources))
	for _, s := range lock.Sources {
		byKey[srcKey{URL: s.URL, Ref: s.Ref}] = s
		byURL[s.URL] = append(byURL[s.URL], s)
	}
	return &FrozenProvider{cache: cache, byKey: byKey, byURL: byURL}
}

// Provide returns the cached worktree for the lockfile pin matching s.
// Errors with ErrPinNotFound if no pin matches. Hydrates the cache from
// the pinned SHA on miss; downstream rev-parse verifies the fetched
// commit is exactly the pin (ErrSHAMismatch on divergence).
func (p *FrozenProvider) Provide(s config.Source) (fs.FS, resolver.ResolvedRef, error) {
	pin, ok := p.lookup(s)
	if !ok {
		return nil, resolver.ResolvedRef{}, fmt.Errorf("%w: %s@%s", ErrPinNotFound, s.URL, s.Ref)
	}
	repoURL, subPath := splitURL(s.URL)
	if !p.cache.has(repoURL, pin.SHA) {
		if err := gitFetch(repoURL, pin.Ref, pin.SHA, p.cache.shaDir(repoURL, pin.SHA)); err != nil {
			return nil, resolver.ResolvedRef{}, err
		}
	}
	fsys, err := p.cache.open(repoURL, pin.SHA, subPath)
	if err != nil {
		return nil, resolver.ResolvedRef{}, err
	}
	return fsys, resolver.ResolvedRef{Ref: pin.Ref, SHA: pin.SHA}, nil
}

func (p *FrozenProvider) lookup(s config.Source) (lockfile.ResolvedSource, bool) {
	if pin, ok := p.byKey[srcKey{URL: s.URL, Ref: s.Ref}]; ok {
		return pin, true
	}
	if s.Ref != "" {
		return lockfile.ResolvedSource{}, false
	}
	// Empty cfg ref: accept the unique pin for this URL. Multiple pins
	// would be ambiguous — refuse rather than guess.
	pins := p.byURL[s.URL]
	if len(pins) == 1 {
		return pins[0], true
	}
	return lockfile.ResolvedSource{}, false
}

// Hydrate fetches every pin that is not already present in the cache.
// Returns errors.Join of all per-pin failures.
func (p *FrozenProvider) Hydrate() error {
	var errs []error
	seen := make(map[string]bool, len(p.byKey))
	for _, pin := range p.byKey {
		repoURL, _ := splitURL(pin.URL)
		key := repoURL + "@" + pin.SHA
		if seen[key] {
			continue
		}
		seen[key] = true
		if p.cache.has(repoURL, pin.SHA) {
			continue
		}
		if err := gitFetch(repoURL, pin.Ref, pin.SHA, p.cache.shaDir(repoURL, pin.SHA)); err != nil {
			errs = append(errs, fmt.Errorf("hydrate %s@%s: %w", pin.URL, pin.Ref, err))
		}
	}
	return errors.Join(errs...)
}

// ErrPinNotFound is returned by FrozenProvider.Provide when the
// requested (URL, Ref) is not in the lockfile.
var ErrPinNotFound = errors.New("source not pinned in lockfile")
