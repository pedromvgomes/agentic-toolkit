---
about: a curation run's grant only bounds what the child may touch, not whether it finishes the job — a run can write notes, exit 0, and leave anchoring/indexing/backlog-clearing undone with nothing after the child exits noticing
saw:
  - source/toolkit/internal/curator/curator.go
  - source/toolkit/internal/curator/prompt.md
  - source/toolkit/internal/cli/memory.go
---

Investigated a report: a `claude -p` curation run delegated to the `memory-curator` subagent,
printed one line, exited 0. Notes were written (including brand-new files under `notes/`
despite the grant naming only `Edit(...)`, no `Write`) but no candidates were removed,
`INDEX.md` was not regenerated, and no anchors were stamped.

**Why `Edit`-only still let new files get written.** `curator.go:238-241` and the existing note
[[curator-write-grant-is-spelled-edit-with-no-mode]] explain this is expected: an `Edit(...)`
rule is consulted by the file-permission check for every file-editing tool, Write included,
while a rule spelled `Write(...)` is not consulted by that check at all. So "no `Write` in the
grant" is not evidence anything went outside the grant — `Edit(<notesDir>/**)` alone permits
brand-new files under `notesDir`.

**Why the rest of the job (anchor, index, rm) can go undone with no error.** The ordered
sequence — write notes, then `agtk memory anchor <name>` per note, then `agtk memory index`,
`agtk memory lint`, then clear `candidates/` — is prose in `prompt.md:113-143` ("After writing
notes: ..."), not something the grant or the driver enforces as a unit. The grant
(`curator.go:216-279`) says what the child *may* do at each step; nothing says it must reach
the later steps once it has done the first. `curator.Run` (`curator.go:400-448`) and the CLI's
`RunE` for `curate` (`source/toolkit/internal/cli/memory.go:800-841`) only inspect
`res.IsError`, which `Result.IsError` documents as "the CLI declared the turn a failure"
(`curator.go:193-195`) — a provider-reported flag, not a check of store state. Nothing after
the child exits reads `agtk memory candidates`, `agtk memory audit`, or diffs `INDEX.md` to
confirm the job actually completed; a turn that stops early after the `Edit` calls and before
the `Bash` calls is indistinguishable, from agtk's side, from one that finished.

No test in `source/toolkit/internal/curator/tests/run_test.go` exercises this either — every
test there drives a fake/stub provider and asserts what grant and prompt a run is given, not
what happens if a real child stops partway through the prompt's steps.

This is a real gap, not (yet) covered by [[curator-write-grant-is-spelled-edit-with-no-mode]]
or [[scoped-anchor-grant-names-each-note-exactly]], both of which are about what the grant
*permits*, not whether the child *finishes*. Worth a postcondition check in `curate` (e.g.
comparing `agtk memory candidates --json` and `agtk memory index`'s current-vs-would-write
state before and after the child runs) if partial runs like this recur.
