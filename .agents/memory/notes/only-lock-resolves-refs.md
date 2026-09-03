---
name: only-lock-resolves-refs
kind: invariant
description: Every command except lock and sync's relock uses FrozenProvider, so nothing else can reach the network to resolve a ref.
anchors:
  - path: internal/cli/lock.go
    blob: 74bd9b9b88e9
  - path: internal/cli/fetch.go
    blob: 1d58f9a1891e
  - path: internal/cli/plan.go
    blob: 3f69225eaee4
  - path: internal/cli/render.go
    blob: 3725b3a58942
  - path: internal/cli/status.go
    blob: 9c0513063466
confidence: verified
---

`LiveProvider` resolves user-supplied refs with `git ls-remote`; `FrozenProvider` serves
strictly from the lockfile and never resolves. Only `lock.go:56` and the relock branch of
`sync.go:75` construct the live one. `fetch`, `plan`, `render` and `status` all construct
the frozen one, so a CI run that has a lockfile can hydrate and render without a ref ever
being re-resolved — that is what makes the committed lockfile authoritative rather than
advisory.

Picking the wrong provider in a new command does not fail loudly. It quietly turns a
reproducible command into one whose output depends on what a branch points at today.
