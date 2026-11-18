---
about: "implement-backlog-item reads exactly one comment on the item, its own progress comment, as state and never as work: only from a write-access author or its own account, with the branch matched against the item's own pattern and every listed commit checked against git"
saw:
  - definitions/skills/implement-backlog-item/SKILL.md
  - definitions/skills/write-backlog-item/SKILL.md
  - docs/adr/0028-a-backlog-item-is-trusted-by-who-wrote-it.md
---

ADR 0028 says comments are never read as work. The progress comment, marked
`<!-- agtk:backlog-progress -->`, is the one exception and is bounded so that it stays state: it
names a branch and commit ids, never a file to change or a command to run. Step 4 of
`implement-backlog-item` takes the oldest such comment whose author is the session's own account or
has write access (`permission` and `role_name` both read, as `pr-review-resolver` does), requires
the branch to match `^feat/backlog-<number>-[a-z0-9-]+$` and to differ from the source, and checks
that every listed commit is an ancestor of `HEAD`. The branch name is derived from the verified
body's Goal and never from the issue title, which anybody who can edit the issue can change after
it was queued.

The branch is rebased onto its source before the review, and the push uses
`--force-with-lease="$branch:$pushed"` with `$pushed` read before the rebase: a bare lease compares
against the remote-tracking ref the fetch just moved, so it always matches and overwrites anything
pushed meanwhile.

Established by five panel review passes over the skill; the first three RED findings were a
mid-work save that could never resume, a `dropped` task still dispatched, and the unpinned lease.
