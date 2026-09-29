---
about: "curate's verify() fails a candidate reported resolved but still staged, yet passes one that is neither reported nor deleted, and nothing but prose says who deletes candidates"
saw:
  - source/toolkit/internal/curator/verify.go
  - source/toolkit/internal/curator/curator.go
  - source/toolkit/internal/curator/prompt.md
  - source/toolkit/internal/cli/memory.go
targets: curate-verification-is-bidirectional
verdict: still-true
---

The note is not stale (`stale: no`) and its claims hold. It is silent on one case, checked here
because a curate run exited 0 with all 24 candidates still on disk.

- Reported-but-present fails: `verify.go:156-163`, `after.candidates[stem]` -> "reported resolved
  but is still in candidates/". Test `curator/tests/verify_test.go:35-43`
  (`TestAResolvedCandidateStillStagedFails`).
- Vanished-but-unreported fails: `verify.go:198-202`.
- Present-and-unreported passes. The candidate loops iterate `r.CandidatesResolved` and
  `before.candidates` filtered by `!after.candidates[id]`, so a candidate that stays on disk and is
  absent from `candidatesResolved` is checked by neither. There is no test for it.
- So a run that promotes notes, lists the candidates NOT in `candidatesResolved` (because it could
  not delete them), and stamps everything, exits 0. An exit 0 with a full `candidates/` is
  therefore consistent with the code as written.
- Why it is not simply a failure: `Options.Limit` (`curator.go:198-206`) and `Options.Notes`
  (`curator.go:165-168`, `task()` at `:637-640` "Leave every other candidate alone") make leftovers
  legitimate; only an unscoped, unlimited run (`task()` `:632-635`) has "the whole backlog" as its
  intended set, and `Options` does not carry that fact into `verify()`.
- Deletion is owned by the curator by prose plus grant only: `prompt.md:143-147` ("Delete every
  candidate you ruled on") and `curator.go:341-343` (`Bash(rm <candidatesDir>/*)`). No ADR assigns
  candidate deletion to anyone (ADR 0003 covers notes/ and the explorer; ADR 0004 covers the grant).
  `git show 04b0c9a --stat` shows the 24 candidates were removed by a hand-made commit (#127).
- Exit-code path: verification error is returned beside a populated Result (`curator.go:586-588`),
  `reportCurateResult` (`cli/memory.go:932-957`) prints Text then returns the error; `IsError`
  maps to the `errMemoryCurate` sentinel (`memory.go:955`, branch `root.go:278`). Before #125 only
  `res.IsError` produced non-zero (`git show ca7dc05^:.../cli/memory.go`, lines ~838-839).
