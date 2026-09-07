---
name: empty-ref-resolves-only-against-a-unique-pin
kind: gotcha
description: An entry with no ref resolves from the lockfile only when its URL is pinned exactly once; a second pin of the same repo makes it read as "not pinned".
anchors:
  - path: internal/sourcestore/provider.go
    blob: 3a0ccf040c3b
  - path: internal/sourcestore/tests/frozen_provider_test.go
    blob: d590f8c51f87
confidence: verified
---

`FrozenProvider.lookup` (`internal/sourcestore/provider.go:103-118`) tries the exact
`(URL, Ref)` key first. A non-empty ref that misses stops there. An *empty* ref falls back to
`p.byURL[s.URL]` and accepts the pin **only if there is exactly one** (`provider.go:112-115`);
two or more returns `false`, and `Provide` reports `ErrPinNotFound` (`provider.go:87-89`) —
the same error as a URL that was never locked at all.

The fallback exists because a user writes no ref while the lockfile records the *resolved*
branch name (`LiveProvider` fills it in from the ls-remote symref,
`internal/sourcestore/git.go:68-70`), so the literal key never matches. Covered by
`TestFrozenProvider_EmptyConfigRef_FallsBackToUniquePinForURL`
(`internal/sourcestore/tests/frozen_provider_test.go:36`).

What breaks: the same repo pinned at two refs is easy to arrive at without meaning to — one
`extends:` writes `@v1`, another entry leaves the ref off and locks `main`. The resolver keys
sources on `(URL, Ref)` (`internal/resolver/sources.go:9`), so `agtk lock` writes both rows
happily. From then on *every* ref-less entry for that URL is unresolvable under the frozen
provider, and the error says "source not pinned in lockfile" while the user is looking at two
lockfile rows for exactly that URL. The refusal is deliberate ("Multiple pins would be
ambiguous — refuse rather than guess", `provider.go:110-111`); the message is what misleads.
The fix in a manifest is to write the ref explicitly, not to relock.

`ErrPinNotFound` is misleading for a second, unrelated reason too — see
[[source-urls-are-matched-byte-for-byte]].
