---
about: curator.Run's postcondition check diffs a pre-run store snapshot against the completion report in both directions, because a claims-only check is defeated by under-reporting, not just by lying
saw:
  - source/toolkit/internal/curator/curator.go
  - source/toolkit/internal/curator/verify.go
---

`curator.Run` snapshots `notes/` and `candidates/` (filenames plus a content
hash per note) before the child starts, then requires a structured
completion report (`candidatesResolved`, `notesRetracted`, `notesTouched`)
from the child's final turn via `agentic.Request.Schema`. `verify()` checks
both directions:

- **claim → disk**: everything the report says was resolved/retracted/
  touched actually matches the store (candidate file gone, note stamped and
  lint-clean).
- **disk → claim**: everything that actually changed on disk since the
  snapshot appears in the report. A note that is new or content-changed but
  absent from `notesTouched`/`notesRetracted` fails verification; a
  candidate that vanished but is absent from `candidatesResolved` fails too.

The second direction is not a nice-to-have. An earlier design (rejected
during review of this change) checked only claim → disk, and a run cut off
partway — the exact failure mode this whole mechanism exists to catch —
writes unstamped notes and then, having stopped before its final turn,
reports nothing at all. `{candidatesResolved: [], notesTouched: []}` passes
every claim → disk check trivially. Only the disk → claim direction catches
a report that under-states what happened, as opposed to one that lies about
overstating it. Weakening `verify()` back to claims-only reopens exactly the
bug this mechanism was built to close.

A `--dry-run` skips verification entirely (no snapshot is taken): it grants
no writing tools, so there is nothing on disk to diff and no report to
demand.
