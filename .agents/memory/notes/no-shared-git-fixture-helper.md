---
name: no-shared-git-fixture-helper
kind: gotcha
description: Five packages each hand-roll their own git fixture builder and two of them are both called newRepo, so a test needing a repo picks one to copy rather than one to import.
anchors:
  - path: internal/cli/tests/helpers_test.go
    blob: 07d966c84806
  - path: internal/sourcestore/tests/helpers_test.go
    blob: 3d68486cde48
  - path: internal/review/tests/repo_test.go
    blob: 9ed0235a997c
  - path: internal/reviewrun/tests/repo_test.go
    blob: 1aada348013e
  - path: internal/reviewrun/run_test.go
    blob: 98e8071c45b3
  - path: internal/curator/tests/run_test.go
    blob: b7092a723cbe
confidence: verified
---

There is no shared fixture package. `grep -rln 'exec.Command("git"' --include='*_test.go' .`
returns nine files across five independently written builders, none sharing code:

- `internal/cli/tests/helpers_test.go:33` — `fixtureRepoFromDir`.
- `internal/sourcestore/tests/helpers_test.go:36` — `fixtureRepo`.
- `internal/review/tests/repo_test.go:22` — `newRepo`, with `(*repo).git` at `:34`.
- `internal/reviewrun/tests/repo_test.go:22` — `newRepo`, with `(*repo).git` at `:35`. A
  *different* package, the same helper name, a near-identical body (both build a `type repo`
  with `git`/`write`/`mkdir`/`commit`). Nothing cross-checks them, so they drift silently.
- `internal/reviewrun/run_test.go:38` — `newGitRepo`, with `(*gitRepo).run` at `:51`: a fifth
  builder in the same package tree as the fourth.

Writing a sixth is the easy mistake; copy-pasting one of the `newRepo` pair is the easier one.

Do not infer "slow package" from this list. `internal/curator/tests` runs no git at all and is
still slow: its cost is `(&agentictest.Fake{...}).Build(t)`
(`internal/curator/tests/run_test.go:27`, from `agentic-driver` at `go.mod:8`), which writes a
`/bin/sh` script into a `t.TempDir()` and execs it once per case.

`-short` does not help — `grep -rn 'testing.Short' --include='*.go' .` has no hits anywhere in
the repo. `make check` is `fmt vet test` (`Makefile:24`), and `go test ./...` runs packages in
parallel, so the suite's wall clock is the single slowest package rather than the sum:
speeding up any other one moves nothing. Iterate with `go test` on the package you are
editing and keep `make check` for the end.

(Replaces an earlier note claiming only `internal/cli/tests` and `internal/sourcestore/tests`
shell out to git; the code-review packages made that false.)
