---
name: only-lock-resolves-refs
kind: invariant
description: Every command except lock and sync's relock uses FrozenProvider, so nothing else can reach the network to resolve a ref.
anchors:
  - path: internal/cli/*.go
    matches:
      - path: internal/cli/fetch.go
        blob: 1d58f9a1891e
      - path: internal/cli/init.go
        blob: 1fd7afc3ea51
      - path: internal/cli/jsonout.go
        blob: cb82873c6a58
      - path: internal/cli/lock.go
        blob: 74bd9b9b88e9
      - path: internal/cli/memory.go
        blob: 08d9bb761374
      - path: internal/cli/paths.go
        blob: d30919b87867
      - path: internal/cli/paths_test.go
        blob: b5f76810ea63
      - path: internal/cli/plan.go
        blob: 3f69225eaee4
      - path: internal/cli/render.go
        blob: 3725b3a58942
      - path: internal/cli/render_error_test.go
        blob: c9d380c11494
      - path: internal/cli/root.go
        blob: 87870af8e371
      - path: internal/cli/status.go
        blob: 9c0513063466
      - path: internal/cli/sync.go
        blob: bdf2025ca832
      - path: internal/cli/update.go
        blob: de55c77114ce
confidence: verified
---

`LiveProvider` resolves user-supplied refs with `git ls-remote`; `FrozenProvider` serves
strictly from the lockfile and never resolves. Only `lock.go:56` and the relock branch of
`sync.go:75` construct the live one. `fetch.go:42`, `plan.go:54`, `render.go:63`,
`status.go:82` and `sync.go:95` construct the frozen one, so a CI run that has a lockfile can
hydrate and render without a ref ever being re-resolved — that is what makes the committed
lockfile authoritative rather than advisory.

Picking the wrong provider in a new command does not fail loudly. It quietly turns a
reproducible command into one whose output depends on what a branch points at today.

The anchor is the glob `internal/cli/*.go`, not the files that construct a provider today:
the claim quantifies over the directory, so what falsifies it is a *new* command file, which
per-file anchors can never notice appearing. See
`docs/adr/0005-glob-anchors-mark-quantified-claims.md`.
