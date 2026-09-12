---
name: only-lock-resolves-refs
kind: invariant
description: Every command except lock and sync's relock uses FrozenProvider, so nothing else can reach the network to resolve a ref.
anchors:
  - path: source/toolkit/internal/cli/*.go
    matches:
      - path: source/toolkit/internal/cli/codereview.go
        blob: 3601595c0eaa
      - path: source/toolkit/internal/cli/codereview_approve.go
        blob: b9a95b835939
      - path: source/toolkit/internal/cli/codereview_approve_test.go
        blob: 057de70cec50
      - path: source/toolkit/internal/cli/codereview_explain_test.go
        blob: a22feda4cd47
      - path: source/toolkit/internal/cli/codereview_init.go
        blob: b7938d155a7e
      - path: source/toolkit/internal/cli/codereview_init_test.go
        blob: db0c644e8f17
      - path: source/toolkit/internal/cli/codereview_initialize.go
        blob: b27674748a1e
      - path: source/toolkit/internal/cli/codereview_json_test.go
        blob: 96483e4bdf63
      - path: source/toolkit/internal/cli/codereview_pr.go
        blob: 3bdb8f071215
      - path: source/toolkit/internal/cli/codereview_pr_test.go
        blob: d1a4f36f7fd7
      - path: source/toolkit/internal/cli/codereview_run.go
        blob: 47db478ff54a
      - path: source/toolkit/internal/cli/codereview_threads_test.go
        blob: a62196c4e28b
      - path: source/toolkit/internal/cli/fetch.go
        blob: 1d58f9a1891e
      - path: source/toolkit/internal/cli/handoff.go
        blob: d1d9642d265c
      - path: source/toolkit/internal/cli/init.go
        blob: 1fd7afc3ea51
      - path: source/toolkit/internal/cli/jsonout.go
        blob: 70e84445dc0a
      - path: source/toolkit/internal/cli/lock.go
        blob: a52fb8f8e2ac
      - path: source/toolkit/internal/cli/memory.go
        blob: 7ab84a4dc55e
      - path: source/toolkit/internal/cli/paths.go
        blob: d30919b87867
      - path: source/toolkit/internal/cli/paths_test.go
        blob: b5f76810ea63
      - path: source/toolkit/internal/cli/plan.go
        blob: 3f69225eaee4
      - path: source/toolkit/internal/cli/render.go
        blob: 3725b3a58942
      - path: source/toolkit/internal/cli/render_error_test.go
        blob: c9d380c11494
      - path: source/toolkit/internal/cli/root.go
        blob: 9b775bf0b883
      - path: source/toolkit/internal/cli/status.go
        blob: 9c0513063466
      - path: source/toolkit/internal/cli/sync.go
        blob: 9220465043b9
      - path: source/toolkit/internal/cli/sync_stale_test.go
        blob: 45a9c7cba741
      - path: source/toolkit/internal/cli/update.go
        blob: b2b0526bee75
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

The anchor is the glob `source/toolkit/internal/cli/*.go`, not the files that construct a provider today:
the claim quantifies over the directory, so what falsifies it is a *new* command file, which
per-file anchors can never notice appearing. See
`docs/adr/0005-glob-anchors-mark-quantified-claims.md`.
