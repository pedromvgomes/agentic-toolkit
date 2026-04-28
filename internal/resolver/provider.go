package resolver

import (
	"io/fs"

	"github.com/pedromvgomes/agentic-toolkit/internal/config"
)

// SourceProvider is the resolver's collaborator: it takes a config.Source
// (URL + optional ref) and returns the source's filesystem along with a
// fully-resolved ref/sha. The provider abstracts over fetching strategy —
// slice 1 ships only test-side implementations; slice 2 introduces the
// disk-backed fetcher.
//
// Filesystem rooting depends on URL shape:
//
//   - Whole-source URLs (config.ConsumerConfig.Source and each entry of
//     config.ConsumerConfig.Externals): the returned fs.FS is rooted at
//     the source's repository root, so entries like
//     "definitions/skills/foo/SKILL.md" resolve directly.
//
//   - Direct-ref URLs (synthesized by the resolver from external preset
//     refs like "skills::github.com/owner/repo/skills/foo"): the returned
//     fs.FS is rooted at the bundle directory itself, so SKILL.md or
//     AGENT.md is the file at the root.
//
// The provider decides how to honor each shape (e.g. via fs.Sub on a
// cached worktree). The resolver does not parse URLs into host/owner/
// repo segments.
type SourceProvider interface {
	Provide(s config.Source) (fs.FS, ResolvedRef, error)
}

// ResolvedRef is the concrete (ref, sha) pair the provider materialized
// for a given Source. When the consumer config left ref empty (default
// branch), Ref echoes the actual branch name the provider chose; SHA is
// the commit that branch pointed to at resolve time.
type ResolvedRef struct {
	Ref string
	SHA string
}
