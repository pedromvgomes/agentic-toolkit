---
name: curate-verification-is-bidirectional
kind: rationale
description: curator verify() diffs the store against the completion report in both directions, because a claims-only check passes a run cut off before its final turn.
anchors:
  - path: source/toolkit/internal/curator/verify.go
    blob: 0719c5632f41
  - path: source/toolkit/internal/curator/curator.go
    blob: b7d9970d93f2
confidence: verified
---

`curator.Run` snapshots `notes/` and `candidates/` (filenames plus a content hash per note)
before the child starts, then requires a structured completion report (`candidatesResolved`,
`notesRetracted`, `notesTouched`; the `Report` type in `curator.go`). `verify()`
(`source/toolkit/internal/curator/verify.go:140`) checks:

- claim to disk: what the report names matches the store (candidate gone, note stamped and
  lint-clean), and what it names was in the starting store, since absence is trivially true of
  something that never existed.
- disk to claim: everything that changed since the snapshot is in the report. A new or changed
  note absent from `notesTouched`/`notesRetracted`, or a vanished candidate absent from
  `candidatesResolved`, fails.

The second direction is the point. A run cut off partway writes unstamped notes and reports
nothing; an empty report satisfies every claim-to-disk check. Only disk-to-claim catches
under-reporting. Reducing `verify()` to claims-only reopens that gap. A `--dry-run` skips
verification (no snapshot, no writing tools).
