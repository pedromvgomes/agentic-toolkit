---
about: an entry with no ref only resolves from the lockfile when its URL is pinned exactly once; a second pin of the same repo turns it into "not pinned"
saw:
  - internal/sourcestore/provider.go
  - internal/sourcestore/tests/frozen_provider_test.go
---

Kind: **gotcha**.

`FrozenProvider.lookup` (`provider.go:102-116`) tries the exact `(URL, Ref)` key first. A
non-empty ref that misses stops there. An *empty* ref falls back to `p.byURL[s.URL]` and
accepts the pin **only if there is exactly one** (`provider.go:111-114`); two or more
returns `false`, and `Provide` reports `ErrPinNotFound` — the same error as a URL that was
never locked at all.

The bridge exists because a user writes no ref, while the lockfile records the resolved
branch name (`LiveProvider` fills it in from the ls-remote symref, `git.go:68-71`), so the
literal key never matches. Covered by
`TestFrozenProvider_EmptyConfigRef_FallsBackToUniquePinForURL`
(`internal/sourcestore/tests/frozen_provider_test.go:36`).

What breaks: the same repo pinned at two refs is easy to arrive at without meaning to — one
`extends:` writes `@v1`, another entry leaves the ref off and locks `main`; the resolver
keys sources on `(URL, Ref)` (`internal/resolver/sources.go:9`) so both rows are written
happily by `agtk lock`. From that point on *every* ref-less entry for that URL is
unresolvable under the frozen provider, and the error says "source not pinned in lockfile"
while the user is looking at two lockfile rows for exactly that URL. The refusal is
deliberate ("Multiple pins would be ambiguous — refuse rather than guess",
`provider.go:109-110`); the message is what misleads. The fix in a manifest is to write the
ref explicitly, not to relock.
