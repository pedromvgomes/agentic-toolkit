---
name: review-is-model-free-reviewrun-is-not
kind: invariant
description: capability.go is the only file in internal/review that names the driver, and internal/reviewrun is the only package that invokes a model — so a credential belongs above reviewrun, in the CLI.
anchors:
  - path: internal/review/*.go
    matches:
      - path: internal/review/builtin.go
        blob: ad571de35ce9
      - path: internal/review/capability.go
        blob: 5f5f11320885
      - path: internal/review/change.go
        blob: bb2a3327cb4c
      - path: internal/review/condition.go
        blob: c3bb9a9b8d4e
      - path: internal/review/detect.go
        blob: 1a74bc0afffc
      - path: internal/review/errors.go
        blob: 0a02e5e95cea
      - path: internal/review/exclude.go
        blob: 8fd1e24bde3c
      - path: internal/review/explain.go
        blob: 906a42c24ba0
      - path: internal/review/fuzz_test.go
        blob: 2a065f2b84ec
      - path: internal/review/git.go
        blob: d8a37c56f1b1
      - path: internal/review/glob.go
        blob: 5989a77ae7cb
      - path: internal/review/language.go
        blob: 25e2c11ca203
      - path: internal/review/manifest.go
        blob: 37a44f279642
      - path: internal/review/parse.go
        blob: dc965e1a4762
      - path: internal/review/pr.go
        blob: e78d117bdb1f
      - path: internal/review/selection.go
        blob: 28cdb6fd6fe2
      - path: internal/review/severity.go
        blob: 54af318571d6
      - path: internal/review/signal.go
        blob: bb4456a5a6e2
      - path: internal/review/symbols.go
        blob: cacc56c152e8
      - path: internal/review/untracked.go
        blob: 8b0a17e460d1
  - path: internal/reviewrun/run.go
    blob: 79519704a9be
  - path: internal/cli/codereview.go
    blob: 1b6a3f78a734
confidence: verified
---

`internal/review` is model-free by construction. `internal/review/capability.go:1-9` is the
only file in that package that imports `agentic-driver`, and it constructs nothing — it
type-asserts a provider's interfaces and calls its argument builders (`CheckCapabilities`
onwards). Manifest parsing, profiling, signal detection and panel selection are all reachable
with no provider CLI and no network, which is what makes `agtk code-review explain`,
`panels` and `signals` free to run on a hook (`internal/cli/codereview.go:13-21`).

The check is `grep -rl agentic-driver internal/review`, which must return exactly
`capability.go`. That grep is the enforcement — no test asserts it — which is why this is
anchored to the whole package: a second file importing the driver breaks the invariant
without any other signal.

`internal/reviewrun` is the complementary half, stated in its package doc
(`internal/reviewrun/run.go:1-11`): "It is the only package in code-review that invokes a
model... Nothing here talks to GitHub." It imports `internal/review` one way, never back. The
driver import inside it lives in `internal/reviewrun/invoke.go`.

**Consequence for anything credential-shaped:** a GitHub App credential belongs above
`reviewrun`, in the CLI layer that calls it — never in `Options`, never through the `invoker`
seam. That direction is guarded by an import-graph test rather than by convention; see
[[credential-guards-are-hand-maintained-lists]].
