<!--
The issue body of a backlog item. The issue's title is the one line saying what this slice must
accomplish; the body starts at the Source line below. Everything in this comment and the
guidance under each heading is for the writer: replace it, and leave none of it in the issue.
-->

Source: `main`.

The branch the work starts from and the pull request targets, **required**. It is the only branch
the body names: the session that takes this item picks the branch it works on itself, creates it
from the source, and records its name in the item's progress comment. For a mid-work save the
source is the branch the work was created from.

Predecessors and the Feature are not written here. They are GitHub's own links on the issue —
blocked-by for each predecessor, sub-issue of the Feature — and a copy in the body would be a
second record that nothing keeps in step with the first.

## Goal

The concrete outcome. What is true when this is done that is not true now.

A stub's body is this section and the `Source:` line, and nothing else.

## Slice

`2 of 3 — the implementing half.` Name the slices still to come, in landing order.

Omit when the Feature is one pull request, and on a mid-work save.

## Plan

What is being built and why this shape. Reference documents by path or URL rather than
restating them.

## Tasks

Landing order. One implementer takes one task.

1. **<what this task achieves>** — `routine`
   Files: `internal/foo/bar.go`, `internal/foo/bar_test.go`
   <what has to be true when it is done>

2. **<what this task achieves>** — `intricate`
   Files: `internal/baz/quux.go`
   <what has to be true when it is done>

A mid-work save may carry one task with no rating; it is read as `routine`.

## Next steps

What the session does first, in order. Concrete enough to act on without reading this whole body
again.

Required when **Tasks** is omitted, and it is what a task-free item is read as: one `routine`
task whose file boundary is what this section names. Name the files it may touch. A body that
leaves both this and **Tasks** out gives the session a goal and no way to start on it.

## Verification

```bash
make check
```

The command that says whether a task landed. One command, runnable as written.

## PR title

```
feat(scope): what the change does
```

Conventional, lower-case, no trailing full stop.

## State

What is already done on the branch and what remains. Anything left uncommitted on purpose, and
why.

## Context and decisions

Constraints, approaches chosen, and dead ends worth not repeating — the reasoning that exists
only in the session writing this. Do not restate what a plan, an ADR, an issue or a commit
already says; point at it.
