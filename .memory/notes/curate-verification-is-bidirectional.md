---
name: curate-verification-is-bidirectional
kind: rationale
description: curator verify() diffs the store against the completion report in both directions, because a claims-only check passes a run cut off before its final turn.
anchors:
  - path: source/toolkit/internal/curator/verify.go
    blob: deee1e17d6ff
  - path: source/toolkit/internal/curator/curator.go
    blob: cb07c4d5ea3f
  - path: docs/adr/0022-agtk-clears-the-candidates-the-curator-processed.md
    blob: 10f104a761bd
confidence: verified
---

`curator.Run` snapshots `notes/` and `candidates/` (a directory listing, `takeSnapshot`,
`verify.go:80-103`, so unparseable candidates are included) before the child starts, then requires
a structured report (`candidatesResolved`, `notesRetracted`, `notesTouched`). `verify()`
(`verify.go:147`, takes `backlog` and `unreadable`) checks:

- claim to disk: what the report names matches the store (note stamped and lint-clean), and what it
  names was in the starting store (`verify.go:163-167`), since absence is trivially true of
  something that never existed. A resolved candidate still staged passes
  (`TestAResolvedCandidateLeftStagedIsCleared`, `tests/verify_test.go:53`); "candidate gone" is not
  a check.
- disk to claim: a new or changed note absent from `notesTouched`/`notesRetracted`, or a vanished
  candidate absent from `candidatesResolved`, fails (`verify.go:201-205`,
  `TestACandidateRemovedButNotReportedFails`, `verify_test.go:321`).

The second direction is the point: a run cut off partway writes unstamped notes and reports nothing,
and an empty report satisfies every claim-to-disk check. `--dry-run` skips verification.

Backlog job only: `Options.backlog()` (`curator.go:219-221`: no notes, no limit, not stale, not
dry run) also fails every readable candidate still in `candidates/` and absent from the report
(`verify.go:214-220`, test `verify_test.go:136`); scoped, limited, stale or dry runs may leave
candidates staged (`verify_test.go:155`).

Who deletes: `clearResolved` (`verify.go:258-278`) `os.Remove`s each resolved candidate still on
disk, with no model call. `Run` calls it only after `verify` returned nil (`curator.go:620-627`), so
a contradicted run clears nothing (`verify_test.go:98`). Cleared ids come back in `Result.Cleared`
(`curator.go:242`) and print as `cleared:` (`cli/memory.go:977-979`). The curator's own deletion is
prose plus grant (`prompt.md:146-150`, `curator.go:362-364`); ADR 0022 records agtk's clearing as the
backstop. See [[unreadable-candidates-surface-in-four-places-and-parsing-stays-strict]].
