# <one line saying what this session must accomplish>

## Goal

The concrete outcome. What is true when this is done that is not true now.

## Predecessor

`PR #91` — this work may not start until that pull request is merged into the base branch.
Branch: `feat/the-next-slice`, from `main`.

Omit this section entirely when the handoff was written with no pull request open. Its absence
means the next session resumes on the current branch immediately.

Where it is present, the branch to create and the base to create it from are **required**. The
next session starts a fresh branch once the predecessor merges, and it has nowhere to get either
name from: the handoff is all it has, and a document that gates the work without saying what to
resume on reads as complete while leaving the session unable to begin.

## Slice

`2 of 3 — the implementing half.` Name the slices still to come, in landing order.

Omit when the plan is one pull request.

## Plan

What is being built and why this shape. Reference documents by path rather than restating them.

## Tasks

Landing order. One implementer takes one task.

1. **<what this task achieves>** — `routine`
   Files: `internal/foo/bar.go`, `internal/foo/bar_test.go`
   <what has to be true when it is done>

2. **<what this task achieves>** — `intricate`
   Files: `internal/baz/quux.go`
   <what has to be true when it is done>

A hand-written handoff may carry one task with no rating; it is read as `routine`.

## Next steps

What the next session does first, in order. Concrete enough to act on without reading this
whole document again.

Required when **Tasks** is omitted, and it is what a task-free handoff is read as: one
`routine` task whose file boundary is what this section names. Name the files it may touch. A
handoff that leaves both this and **Tasks** out gives the next session a goal and no way to
start on it.

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

Branch, what is already done, what remains. Anything uncommitted and why.

## Context and decisions

Constraints, approaches chosen, and dead ends worth not repeating — the reasoning that exists
only in the session writing this. Do not restate what a plan, an ADR or a commit already says;
point at it.
