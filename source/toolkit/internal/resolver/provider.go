package resolver

import (
	"io/fs"

	"github.com/pedromvgomes/agentic-toolkit/internal/sourceref"
)

// SourceProvider is the resolver's collaborator: it takes a sourceref.Source
// (URL + optional ref) and returns the source's filesystem along with a
// fully-resolved ref/sha. The provider abstracts over fetching strategy —
// the disk-backed implementation lives in internal/sourcestore.
//
// Filesystem rooting depends on URL shape:
//
//   - Whole-source URLs (no `.git/` substring, e.g. "github.com/owner/repo"):
//     the returned fs.FS is rooted at the source's repository root, so
//     entries like "definitions/skills/foo/SKILL.md" resolve directly.
//
//   - Direct-ref URLs (containing `.git/` plus an in-repo path, e.g.
//     "github.com/owner/repo.git/skills/foo"): the returned fs.FS is rooted
//     at the in-repo path itself, so SKILL.md or AGENT.md (or any other
//     file at that path) is at the FS root.
//
// The provider decides how to honor each shape (e.g. via fs.Sub on a
// cached worktree). The resolver does not parse URLs into host/owner/
// repo segments.
type SourceProvider interface {
	Provide(s sourceref.Source) (fs.FS, ResolvedRef, error)
}

// ResolvedRef is the concrete (ref, sha) pair the provider materialized
// for a given Source. When the consumer config left ref empty (default
// branch), Ref echoes the actual branch name the provider chose; SHA is
// the commit that branch pointed to at resolve time.
type ResolvedRef struct {
	Ref string
	SHA string
}
