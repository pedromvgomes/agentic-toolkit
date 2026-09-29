---
about: "verify() passes a resolved candidate that is still staged and agtk deletes it afterwards; only the backlog job fails on an unreported leftover"
saw:
  - source/toolkit/internal/curator/verify.go
  - source/toolkit/internal/curator/curator.go
  - source/toolkit/internal/curator/prompt.md
  - source/toolkit/internal/curator/tests/verify_test.go
  - docs/adr/0021-agtk-clears-the-candidates-the-curator-processed.md
targets: curate-verification-is-bidirectional
verdict: now-false
---

The note's core claim holds and is to be kept: `verify()` checks both directions, and a vanished
candidate absent from `candidatesResolved` still fails (`verify.go:201-205`, test
`curator/tests/verify_test.go:321` `TestACandidateRemovedButNotReportedFails`). One clause is
false and the body needs a rewrite, not a deletion: the claim-to-disk list "(candidate gone, note
stamped and lint-clean)". A resolved candidate that is still in `candidates/` passes, so
"candidate gone" is not a check. The pointer is also `verify()` at `verify.go:147`, not `:140`; it
takes `backlog` and `unreadable` parameters.

What holds now:

- Resolved and still staged: passes verification. The `r.CandidatesResolved` loop
  (`verify.go:163-167`) only requires that the id was in the pre-run snapshot. Test
  `verify_test.go:53` (`TestAResolvedCandidateLeftStagedIsCleared`).
- Who deletes: `clearResolved` (`verify.go:258-278`) removes each resolved candidate that is still
  on disk with `os.Remove`, no model call. `Run` calls it only after `verify` returned nil
  (`curator.go:620-627`), so a run the store contradicts clears nothing (`verify_test.go:98`
  `TestNothingIsClearedWhenVerificationFails`) and a dry run clears nothing (`curator.go:620`).
  The ids it removed come back in `Result.Cleared` (`curator.go:242`) and the CLI prints each as a
  `cleared:` line (`cli/memory.go:977-979`). A candidate the run already deleted is skipped
  (`verify_test.go:80`).
- Unreported and still staged: fails only for the backlog job. `Options.backlog()`
  (`curator.go:219-221`) is `len(Notes)==0 && Limit<=0 && !Stale && !DryRun`, and `verify` fails
  every candidate left in `candidates/` and absent from the report when it is true
  (`verify.go:214-220`, test `verify_test.go:136`). A scoped, limited, stale or dry run may leave
  candidates staged and passes (`verify_test.go:155`).
- The snapshot is a directory listing (`takeSnapshot`, `verify.go:80-103`), so it includes
  candidates that do not parse; the backlog check excludes them (`verify.go:215`) and clearing
  skips them (`curator.go:624`).
- Deletion by the curator itself is still prose plus grant (`prompt.md:146-150`,
  `curator.go:362-364`); ADR 0021 records that `agtk` clearing is the deterministic backstop and
  the grant stays.
