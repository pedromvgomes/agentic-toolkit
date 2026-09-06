---
about: hydrating a lockfile pin fetches its ref and only then checks the sha, so a pin whose branch moved is unfetchable on a cold cache
saw:
  - internal/sourcestore/provider.go
  - internal/sourcestore/git.go
  - internal/sourcestore/tests/frozen_provider_test.go
---

Kind: **gotcha**.

`FrozenProvider` never asks the remote for the pinned commit. Both call sites pass the
pin's *ref* as the thing to fetch and the pin's *sha* only as an expectation:

- `provider.go:91` — `gitFetch(repoURL, pin.Ref, pin.SHA, p.cache.shaDir(repoURL, pin.SHA))`
- `provider.go:133` — the same call inside `Hydrate()`

and `gitFetch` (`git.go:78`) does `git init` + `git fetch --quiet --depth 1 <url> <ref>`
(`git.go:102`) + `git checkout FETCH_HEAD`, then `rev-parse HEAD` and compares
(`git.go:108-116`). Divergence is a hard `ErrSHAMismatch` — deliberate, per the doc
comment at `git.go:147-151`, and pinned by
`TestFrozenProvider_SHAMismatch_HardFails` (`internal/sourcestore/tests/frozen_provider_test.go:88`).

What this means, and what a reader gets wrong: the lockfile is reproducible only while the
pinned ref still resolves to the pinned sha, or while the cache is already warm
(`p.cache.has(...)` short-circuits the fetch at `provider.go:90`). Pin a *branch* — which
is what an empty `ref:` produces, since `LiveProvider` records the resolved default-branch
name (`git.go:68-71`) — and the very next upstream push makes that lockfile row unfetchable
on any machine that has not already cached it. `agtk fetch` in clean CI then fails with
"sha mismatch", which reads like corruption rather than "the branch moved".

`--depth 1` on a ref is also why fetching the sha directly is not a drop-in fix: it needs
the server to allow reachable-sha1-in-want, which is off by default on many forges.

Related wrinkle in the same mechanism: `Hydrate()` dedupes on `repoURL + "@" + sha`
(`provider.go:125-129`) while iterating a *map*, so when one sha is pinned under two refs
the ref used for the fetch is whichever the map yields first. If one of those refs has been
deleted upstream, hydration fails intermittently rather than consistently.
