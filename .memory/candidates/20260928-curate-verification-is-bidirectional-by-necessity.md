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

The second direction is not a nice-to-have. A check of claim → disk alone
passes a run cut off partway — the exact failure mode this mechanism exists
to catch. Such a run writes unstamped notes and then, having stopped before
its final turn, reports nothing at all: `{candidatesResolved: [],
notesTouched: []}` satisfies every claim → disk check trivially. Only the
disk → claim direction catches a report that under-states what happened, as
opposed to one that overstates it. Reducing `verify()` to claims-only
reopens exactly the gap this mechanism closes.

The claim → disk direction also requires what a report names to have been in
the store the run started from: a candidate reported resolved must have been
staged and a note reported retracted must have been in `notes/`, because
absence afterwards is trivially true of something that never existed.

A `--dry-run` skips verification entirely (no snapshot is taken): it grants
no writing tools, so there is nothing on disk to diff and no report to
demand.
