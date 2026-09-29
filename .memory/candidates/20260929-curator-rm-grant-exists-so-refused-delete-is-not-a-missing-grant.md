---
about: "the curator grant includes a candidates-scoped rm, and agtk clears the resolved candidates itself, so a refused or mismatched curator rm does not leave the backlog uncleared"
saw:
  - source/toolkit/internal/curator/curator.go
  - source/toolkit/internal/curator/verify.go
  - source/toolkit/internal/curator/tests/curator_test.go
  - source/toolkit/internal/cli/memory.go
  - source/toolkit/internal/memory/store.go
targets: bash-writes-are-refused-inside-the-working-directory
verdict: unchecked
---

The provider-guard claim itself was not reproduced (no claude session available), so the verdict
is `unchecked` for that part. Pointers in the note have moved: the deletion grants are
`Bash(rm <candidatesDir>/*)` at `curator.go:362-364` and the notes twin at `:370-372` (note says
`:268`/`:276`). Asserted by `curator/tests/curator_test.go:116-118`.

- `CandidatesDir` is `store.CandidatesPath()` (`cli/memory.go:827,849`), which is
  `filepath.Join(s.Root, "candidates")` (`memory/store.go:93`), and `memory.New` makes Root absolute
  by joining the project root (`store.go:31-38`). So the grant is an ABSOLUTE-path pattern
  (`Bash(rm /abs/.memory/candidates/*)`). Inference, not tested: a model that issues a relative
  `rm .memory/candidates/x.md`, `rm -f ...` or `git rm ...` may not match it. No test exercises a
  real provider matching this pattern against such commands.
- A refused or mismatched candidate `rm` does not leave a resolved candidate staged. `Run` calls
  `clearResolved` after `verify` passes (`curator.go:620-627`); it deletes with `os.Remove` from
  agtk's own process (`verify.go:268`), so neither the grant nor the provider's guard is consulted.
  It covers only ids in `candidatesResolved` that were staged when the run started
  (`verify.go:255-257`), so a candidate the run failed to rule on stays.
- The notes twin is not backstopped. A refused `rm` on a retracted note leaves the file in
  `notes/`, and `verify` fails the run for it (`verify.go:168-173`, "reported retracted but is
  still in notes/") before `clearResolved` runs, so nothing is cleared either.
- An uncleared `candidates/` after a passing run therefore means the report did not name the
  candidate, or the run failed verification or was a dry run; it is not by itself evidence of a
  refused `rm`. On a backlog run, an unreported leftover fails verification (`verify.go:214-220`).

Not established: whether the provider's working-directory guard refuses `rm` under the staging
directory at all; the note records a report of it that did not reproduce.
