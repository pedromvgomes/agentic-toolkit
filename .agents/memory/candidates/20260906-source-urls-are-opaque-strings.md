---
about: source URLs are matched byte-for-byte everywhere — nothing canonicalizes scheme or the .git suffix, so two spellings of one repo are two sources
saw:
  - internal/sourcestore/*.go
  - internal/resolver/*.go
  - internal/lockfile/*.go
---

Kind: **invariant** (quantified — see below).

Quantified claim: **no file in `internal/sourcestore/`, `internal/resolver/` or
`internal/lockfile/` canonicalizes a source URL.** The only transformation any of them
performs is splitting on the `.git/` substring, and it appears twice, duplicated:
`splitURL` (`internal/sourcestore/url.go:33`) and `splitGitURL`
(`internal/resolver/resolver.go:517`) — same three lines, two packages. `gitTransportURL`
(`git.go:170`) prefixes `https://` for the git subprocess but is deliberately *not* applied
to anything stored: the scheme-less form is what reaches the cache key and the lockfile.

Everything downstream is therefore an exact-string match:

- cache directory = `sha256(repoURL)` of the string as written (`cache.go:48-51`)
- `FrozenProvider.byKey` / `byURL` are plain maps keyed on the string (`provider.go:71-76`,
  `lookup` at `:102`)
- the resolver's `sourceTable` keys on `(URL, Ref)` verbatim (`internal/resolver/sources.go:9`)

So `github.com/o/r`, `github.com/o/r.git` and `https://github.com/o/r` are three distinct
sources: three cache trees, three lockfile rows, three fetches of identical content. Worse,
a lockfile written when the manifest said one spelling does not satisfy the manifest after
someone "tidies up" the spelling — `lookup` misses and `Provide` returns `ErrPinNotFound`
("source not pinned in lockfile"), which reads as *never locked* rather than *spelled
differently*. Editing a URL in a manifest is therefore always a relock, never a no-op.

This is a decision, not an oversight: `cache.go:12-20` states that divergent spellings are
kept apart on purpose, to preserve provenance and avoid canonicalization.

The second half of the invariant: `.git/` is the *only* boundary marker between repo and
in-repo path (`internal/sourcestore/url.go:1-24`). There is no host list and no
segment-counting. `github.com/o/r/skills/foo` — no `.git` — is not "repo + path"; it is a
whole repo URL named `github.com/o/r/skills/foo`, and it fails at fetch time against the
forge, not at parse time. Both `extends:` and per-category URL entries do reject a URL with
no in-repo path outright (`resolver.go:238`, `resolver.go:340`), so the confusing case is
specifically the one that *looks* like a path but never had `.git`.
