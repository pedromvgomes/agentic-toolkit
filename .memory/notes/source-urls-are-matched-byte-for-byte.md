---
name: source-urls-are-matched-byte-for-byte
kind: invariant
description: Nothing canonicalizes a source URL's scheme or .git suffix, so two spellings of one repo are two sources with two cache trees and two lockfile rows.
anchors:
  - path: source/toolkit/internal/sourcestore/*.go
    matches:
      - path: source/toolkit/internal/sourcestore/cache.go
        blob: 0427b620857b
      - path: source/toolkit/internal/sourcestore/git.go
        blob: fd93beacf3e2
      - path: source/toolkit/internal/sourcestore/provider.go
        blob: 3a0ccf040c3b
      - path: source/toolkit/internal/sourcestore/transport_test.go
        blob: 3104125a1655
      - path: source/toolkit/internal/sourcestore/url.go
        blob: 31cac94af64f
  - path: source/toolkit/internal/resolver/*.go
    matches:
      - path: source/toolkit/internal/resolver/provider.go
        blob: 008c721626cb
      - path: source/toolkit/internal/resolver/requires.go
        blob: de9ff0d03dc9
      - path: source/toolkit/internal/resolver/resolver.go
        blob: 022646e73709
      - path: source/toolkit/internal/resolver/sources.go
        blob: 91aa5dda5f2e
      - path: source/toolkit/internal/resolver/types.go
        blob: f324cf4d5af4
  - path: source/toolkit/internal/lockfile/*.go
    matches:
      - path: source/toolkit/internal/lockfile/errors.go
        blob: 7473fe80fe52
      - path: source/toolkit/internal/lockfile/parser.go
        blob: 2039f12de5d8
      - path: source/toolkit/internal/lockfile/types.go
        blob: 8b4fcfab2c52
confidence: verified
---

Quantified claim: **no file in `source/toolkit/internal/sourcestore/`, `source/toolkit/internal/resolver/` or
`source/toolkit/internal/lockfile/` canonicalizes a source URL.** The only transformation any of them
performs is splitting on the `.git/` substring, and it appears twice, duplicated — `splitURL`
(`source/toolkit/internal/sourcestore/url.go:33`) and `splitGitURL` (`source/toolkit/internal/resolver/resolver.go:517`),
the same three lines in two packages. `gitTransportURL`
(`source/toolkit/internal/sourcestore/git.go:216`) prefixes `https://` for the git subprocess but is
deliberately *not* applied to anything stored: the scheme-less form is what reaches the cache
key and the lockfile. The globs are deliberate — a new file in any of the three that
normalizes URLs is what falsifies this, and per-file anchors could never notice it
(docs/adr/0005).

Everything downstream is an exact-string match:

- cache directory = `sha256(repoURL)` of the string as written
  (`source/toolkit/internal/sourcestore/cache.go:47-50`)
- `FrozenProvider.byKey` / `byURL` are plain maps keyed on the string
  (`source/toolkit/internal/sourcestore/provider.go:75-76`, `lookup` at `:103`)
- the resolver's `sourceTable` keys on `(URL, Ref)` verbatim
  (`source/toolkit/internal/resolver/sources.go:9`)

So `github.com/o/r`, `github.com/o/r.git` and `https://github.com/o/r` are three distinct
sources: three cache trees, three lockfile rows, three fetches of identical content. Worse, a
lockfile written when the manifest said one spelling does not satisfy the manifest after
someone "tidies up" the spelling — `lookup` misses and `Provide` returns `ErrPinNotFound`
("source not pinned in lockfile"), which reads as *never locked* rather than *spelled
differently*. Editing a URL in a manifest is therefore always a relock, never a no-op.

This is a decision, not an oversight: `source/toolkit/internal/sourcestore/cache.go:12-20` states that
divergent spellings are kept apart on purpose, to preserve provenance and avoid
canonicalization.

Second half of the invariant: `.git/` is the *only* boundary marker between repo and in-repo
path (`source/toolkit/internal/sourcestore/url.go:10-23`). There is no host list and no segment-counting.
`github.com/o/r/skills/foo` — no `.git` — is not "repo + path"; it is a whole repo URL named
`github.com/o/r/skills/foo`, and it fails at fetch time against the forge, not at parse time.
Both `extends:` and per-category URL entries reject a URL with *no* in-repo path outright
(`source/toolkit/internal/resolver/resolver.go:237-239`, `:339-341`), so the confusing case is specifically
the one that *looks* like a path but never had `.git`.

See also [[empty-ref-resolves-only-against-a-unique-pin]] for the other way `ErrPinNotFound`
misdescribes what happened.
