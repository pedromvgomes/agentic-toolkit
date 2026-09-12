---
name: review-is-model-free-reviewrun-is-not
kind: invariant
description: capability.go is the only file in source/toolkit/internal/review that names the driver, and source/toolkit/internal/reviewrun is the only package that invokes a model — so a credential belongs above reviewrun, in the CLI.
anchors:
  - path: source/toolkit/internal/review/*.go
    matches:
      - path: source/toolkit/internal/review/builtin.go
        blob: 04e622ab978f
      - path: source/toolkit/internal/review/capability.go
        blob: b4a67453292c
      - path: source/toolkit/internal/review/change.go
        blob: dab7ddcffb78
      - path: source/toolkit/internal/review/condition.go
        blob: 22557cc37b26
      - path: source/toolkit/internal/review/detect.go
        blob: 1a74bc0afffc
      - path: source/toolkit/internal/review/errors.go
        blob: e261fd4d030a
      - path: source/toolkit/internal/review/exclude.go
        blob: 449a1d7261d1
      - path: source/toolkit/internal/review/explain.go
        blob: 906a42c24ba0
      - path: source/toolkit/internal/review/fuzz_test.go
        blob: 2a065f2b84ec
      - path: source/toolkit/internal/review/git.go
        blob: d8a37c56f1b1
      - path: source/toolkit/internal/review/glob.go
        blob: 4ade8fb042ce
      - path: source/toolkit/internal/review/language.go
        blob: 25e2c11ca203
      - path: source/toolkit/internal/review/manifest.go
        blob: b720da152793
      - path: source/toolkit/internal/review/parse.go
        blob: bc3bb102eadd
      - path: source/toolkit/internal/review/pr.go
        blob: e78d117bdb1f
      - path: source/toolkit/internal/review/selection.go
        blob: fabfd46969de
      - path: source/toolkit/internal/review/severity.go
        blob: 54af318571d6
      - path: source/toolkit/internal/review/signal.go
        blob: bb4456a5a6e2
      - path: source/toolkit/internal/review/symbols.go
        blob: cacc56c152e8
      - path: source/toolkit/internal/review/untracked.go
        blob: 8b0a17e460d1
  - path: source/toolkit/internal/reviewrun/run.go
    blob: d4cdd11b4fc6
  - path: source/toolkit/internal/cli/codereview.go
    blob: 3601595c0eaa
  - path: source/toolkit/internal/cli/tests/credential_surface_test.go
    blob: 8e777fd5dfaf
  - path: source/toolkit/internal/cli/tests/deterministic_surface_test.go
    blob: ceeec7395429
confidence: verified
---

`source/toolkit/internal/review` is model-free by construction.
`source/toolkit/internal/review/capability.go` is the only file in that package that imports
`agentic-driver` (the import is at `:14`, and `:3-9` says why), and it constructs nothing — it
type-asserts a provider's interfaces and calls its argument builders (`CheckCapabilities`
onwards). Manifest parsing, profiling, signal detection and panel selection are all reachable
with no provider CLI and no network, which is what makes `agtk code-review explain`,
`panels` and `signals` free to run on a hook (`source/toolkit/internal/cli/codereview.go:14-27`).

**Model-free is not the same as offline**, and the distinction has one exception worth
knowing: `explain --pr` is model-free but *not* hook-safe. It reads the pull request and
fetches its head, because base, head and context are what naming a PR decides and none of the
three is knowable without asking GitHub — so it needs the App registration and fails without
one, before a panel has run (`source/toolkit/internal/cli/codereview.go:19-24`). Bare
`explain` is safe on a hook path; `explain --pr` is not.

The quick check is `grep -rl agentic-driver source/toolkit/internal/review`, which must return
exactly `capability.go`. The grep is no longer the only enforcement:
`TestTheDriverIsReachedThroughNamedSeamsOnly`
(`source/toolkit/internal/cli/tests/deterministic_surface_test.go:43-86`) parses the imports of
every non-test `.go` under `source/toolkit/internal` and fails on any that names the driver
module outside `modelInvokingPackages` (`:98-101`, `curator/` and `reviewrun/`) and the
`driverSeams` allowlist (`:29-32`), which is exactly `provider/provider.go` and
`review/capability.go`. Its own comment (`:40-42`) says it turns ADR 0002's "checkable by
grep" into checked-by-imports, so a file merely naming the module in a comment or an error
string is not a violation.

**The grep is still the broader of the two**, in one direction that matters: the test skips
`_test.go` files deliberately (`:64-66`, so a test can build a fake provider), so a *test* file
in `internal/review` importing the driver fails the grep and passes the test.

This note is anchored to the whole package with a glob because the claim quantifies — "the
only file in the package" is falsified by a file that does not exist yet, which a per-file
anchor could never notice appearing.

`source/toolkit/internal/reviewrun` is the complementary half, stated in its package doc
(`source/toolkit/internal/reviewrun/run.go:1-11`): "It is the only package in code-review that invokes a
model... Nothing here talks to GitHub." It imports `source/toolkit/internal/review` one way, never back. The
driver import inside it lives in `source/toolkit/internal/reviewrun/invoke.go`.

**Consequence for anything credential-shaped:** a GitHub App credential belongs above
`reviewrun`, in the CLI layer that calls it — never in `Options`, never through the `invoker`
seam. That direction is guarded by an import-graph test rather than by convention:
`TestTheModelInvokingPackagesCannotReachTheCredential`
(`source/toolkit/internal/cli/tests/credential_surface_test.go:52-69`) runs `go list -deps` over
`reviewrun`, `curator` and `review` and fails if `internal/githubapp` appears transitively. The
package list it walks is a hand-typed slice, so a *new* model-invoking package is uncovered
until someone adds it — see [[credential-guards-are-hand-maintained-lists]].
