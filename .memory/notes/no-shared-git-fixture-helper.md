---
name: no-shared-git-fixture-helper
kind: gotcha
description: Every package hand-rolls its own git fixture builder — eight and counting, two of them both called newRepo — so a test needing a repo picks one to copy rather than one to import.
anchors:
  - path: source/toolkit/internal/cli/tests/helpers_test.go
    blob: 07d966c84806
  - path: source/toolkit/internal/cli/tests/handoff_test.go
    blob: f84b6fc5c818
  - path: source/toolkit/internal/cli/tests/codereview_run_test.go
    blob: d04a113afe92
  - path: source/toolkit/internal/cli/codereview_init_test.go
    blob: 4b36a7202e2d
  - path: source/toolkit/internal/cli/codereview_pr_test.go
    blob: 9f5ffecbf3c0
  - path: source/toolkit/internal/sourcestore/tests/helpers_test.go
    blob: 3d68486cde48
  - path: source/toolkit/internal/review/tests/repo_test.go
    blob: 9ed0235a997c
  - path: source/toolkit/internal/reviewrun/tests/repo_test.go
    blob: 1aada348013e
  - path: source/toolkit/internal/reviewrun/run_test.go
    blob: e9386add686e
  - path: source/toolkit/internal/handoff/handoff_test.go
    blob: 21c55c914f7e
  - path: source/toolkit/internal/curator/tests/run_test.go
    blob: 1b23a7450517
confidence: verified
---

There is no shared fixture package. `grep -rln 'exec.Command("git"' --include='*_test.go' .`
returns twelve files across at least eight independently written builders, none sharing code:

- `source/toolkit/internal/cli/tests/helpers_test.go:33` — `fixtureRepoFromDir`.
- `source/toolkit/internal/sourcestore/tests/helpers_test.go:36` — `fixtureRepo`.
- `source/toolkit/internal/review/tests/repo_test.go:22` — `newRepo`, with `(*repo).git` at `:34`.
- `source/toolkit/internal/reviewrun/tests/repo_test.go:22` — `newRepo`, with `(*repo).git` at `:35`. A
  *different* package, the same helper name, a near-identical body (both build a `type repo`
  with `git`/`write`/`mkdir`/`commit`). Nothing cross-checks them, so they drift silently.
- `source/toolkit/internal/reviewrun/run_test.go:38` — `newGitRepo`, with `(*gitRepo).run` at `:51`: a fifth
  builder in the same package tree as the fourth.
- `source/toolkit/internal/handoff/handoff_test.go:14` — `repo`, with `run` at `:351`.
- `source/toolkit/internal/cli/tests/handoff_test.go:13` — `handoffRepo`.
- `source/toolkit/internal/cli/tests/codereview_run_test.go:13` — `reviewRepo`.
- `source/toolkit/internal/cli/codereview_init_test.go:17` — `initRepo`.
- `source/toolkit/internal/cli/codereview_pr_test.go:71` — `prRepo`.

Plus inline git runners that are not even factored into a builder:
`source/toolkit/internal/cli/tests/codereview_test.go:15` and
`source/toolkit/internal/cli/tests/deterministic_surface_test.go:248,258`.

"Writing a sixth is the easy mistake" is no longer hypothetical — the handoff and code-review
work added five more without touching this list, which is the drift the note predicted.
Copy-pasting one of the `newRepo` pair remains the easier mistake.

**The anchors under-report by construction, and deliberately.** The claim quantifies — *every*
package rolls its own — so what falsifies it is a builder in a file that does not exist yet,
which a per-file anchor can never notice; that is exactly how the count above went stale.
The quantified form wants a glob per `docs/adr/0005-glob-anchors-mark-quantified-claims.md`,
but the only globs that would cover it are `source/toolkit/internal/cli/*_test.go` and
`source/toolkit/internal/cli/tests/*_test.go`, which expand to 32 files of which ~4 are builders. Tried and
reverted: the note went stale on every unrelated test edit in two of the busiest directories in
the repo, and churn a reader learns to ignore costs more than the signal is worth. Re-derive
the list with the grep above rather than trusting the anchors to flag a new builder.

Do not infer "slow package" from this list. `source/toolkit/internal/curator/tests` runs no git at all and is
still slow: its cost is `(&agentictest.Fake{...}).Build(t)`
(`source/toolkit/internal/curator/tests/run_test.go:27`, from `agentic-driver` at
`source/toolkit/go.mod:15`), which writes a
`/bin/sh` script into a `t.TempDir()` and execs it once per case.

`-short` does not help — `grep -rn 'testing.Short' --include='*.go' .` has no hits anywhere in
the repo. `make check` is `fmt vet test` (`Makefile:34`), and `go test ./...` runs packages in
parallel, so the suite's wall clock is the single slowest package rather than the sum:
speeding up any other one moves nothing. Iterate with `go test` on the package you are
editing and keep `make check` for the end.

(Replaces an earlier note claiming only `source/toolkit/internal/cli/tests` and `source/toolkit/internal/sourcestore/tests`
shell out to git; the code-review packages made that false.)
