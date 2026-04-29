// Package sourcestore implements an on-disk source cache and the two
// resolver.SourceProvider variants the CLI needs:
//
//   - LiveProvider resolves user-supplied refs to SHAs via `git ls-remote`
//     and hydrates the cache. Used by `agtk lock`.
//   - FrozenProvider serves Provide calls strictly from a lockfile, never
//     resolving refs. It hydrates the cache for any pin that's missing
//     locally. Used by `agtk plan` and `agtk fetch`.
//
// # URL grammar
//
// Source URLs come in two shapes. The `.git/` substring is the explicit
// boundary between the repository URL and an optional in-repo bundle
// path:
//
//	github.com/owner/repo                 # whole-source, no `.git`
//	github.com/owner/repo.git             # whole-source, explicit `.git`
//	github.com/owner/repo.git/skills/foo  # direct ref into bundle dir
//
// Whole-source URLs return an fs.FS rooted at the repository top.
// Direct-ref URLs return an fs.FS rooted at the bundle directory itself.
// The boundary is git-native and host-agnostic — there is no list of
// known hosts and no segment-counting heuristic.
package sourcestore

import "strings"

// splitURL separates a source URL into (repoURL, subPath).
//
// If `.git/` is present, the repo URL retains its `.git` suffix and the
// in-repo path follows. If absent, the entire input is the repo URL and
// subPath is empty.
func splitURL(u string) (repoURL, subPath string) {
	const sep = ".git/"
	if i := strings.Index(u, sep); i >= 0 {
		return u[:i+len(".git")], strings.TrimRight(u[i+len(sep):], "/")
	}
	return u, ""
}
