---
about: "the curator grant does include a candidates-scoped rm, so an uncleared backlog is not a missing grant; the causes are unconfirmed"
saw:
  - source/toolkit/internal/curator/curator.go
  - source/toolkit/internal/curator/tests/curator_test.go
  - source/toolkit/internal/cli/memory.go
  - source/toolkit/internal/memory/store.go
targets: bash-writes-are-refused-inside-the-working-directory
verdict: unchecked
---

Note is `stale: yes` (curator.go moved). Re-checked what was cheap; the provider-guard claim itself
was not reproduced (no claude session available), so the verdict is `unchecked` for that part.

Still true / pointers moved:
- The deletion grants are `Bash(rm <candidatesDir>/*)` at `curator.go:341-343` and the notes twin at
  `:349-351` (note says `:268`/`:276`). Asserted by `curator/tests/curator_test.go:117-118`.
- `CandidatesDir` is `store.CandidatesPath()` (`cli/memory.go:819,841`), which is
  `filepath.Join(s.Root, "candidates")` (`memory/store.go:93`), and `memory.New` makes Root absolute
  by joining the project root (`store.go:30-32`). So the grant is an ABSOLUTE-path pattern
  (`Bash(rm /abs/.memory/candidates/*)`). Inference, not tested: a model that issues a relative
  `rm .memory/candidates/x.md`, `rm -f ...` or `git rm ...` may not match it. No test exercises a
  real provider matching this pattern against such commands.
- The grant has existed since #73 (`git log -S'rm ' -- curator.go` -> 5006431), so a build older
  than that would lack it.
- The note already records an earlier report of exactly this symptom (backlog uncleared, rm refused
  by a working-directory guard) that did not reproduce. It remains unexplained.

Not established: which of {pattern mismatch, provider working-directory guard, older build} hit the
run with 24 leftovers.
