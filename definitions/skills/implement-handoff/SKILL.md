---
name: implement-handoff
description: |
  Carry out the work a handoff describes: check the pull request it waits on, read its tasks, and land them one at a time — one
  implementer subagent per task, its diff reviewed and verified before it is committed — then run the capped review loop and open
  the pull request. Invoked by the session-start hook when a handoff is waiting. Trigger on "implement the handoff", "continue the
  handoff", "pick up the handoff", "run the handoff in handoff/".
requires:
  - agents/task-implementer
  - skills/review-implementation
  - skills/open-pr
  - skills/write-handoff
  - instructions/git
---

# Implement handoff

You are the **coordinator**. You read the handoff, you dispatch one implementer per task, and
you decide what lands.

Stay the main session. Nesting is allowed three layers deep, so nothing stops you delegating
this role — but your model is the only one in the flow that outlives a single call, and a
delegated coordinator would hold every implementer's transcript in the context this flow exists
to keep clear.

## 1 — Choose the handoff

Depth one in `handoff/`; `handoff/done/` is consumed and not a candidate. More than one waiting
is a question for the user, never a guess — the oldest is not reliably the next.

## 2 — Check the predecessor

If the handoff names a predecessor pull request, that work may not start until the pull request
is **merged into the base branch**. Check it:

```bash
gh pr view <N> --json state,mergedAt,baseRefName
```

Not merged — open, closed, draft, anything — **stop**. Say which pull request is pending and
that the work resumes once it lands. Do not start the tasks, do not mark the handoff consumed.

Merged: the branch has to move onto the updated base. **Say what you are about to run and wait
for a yes** before touching the branch — this is the one step that changes where the user is
standing, and it is cheap to confirm and expensive to surprise.

```bash
git status --porcelain     # must be empty
git fetch origin
git checkout <base> && git merge --ff-only origin/<base>
git checkout -b <branch named in the handoff>
```

A dirty worktree refuses the whole sequence. Say what is uncommitted and stop.

A handoff with no predecessor starts immediately, on the branch it was written on.

## 3 — Build the task list

From the handoff, in landing order. Each task carries the files it may touch, a `routine` or
`intricate` rating, and the verification command. A hand-written handoff carrying none of this
is one `routine` task whose boundary is what its next steps describe.

Show the list before starting. The user is entitled to see the plan being executed, not only
its result.

## 4 — One task at a time

For each task, in order:

**Dispatch** exactly one `task-implementer`, with the task, its file boundary, and the
verification command. Pass `model: opus` when the task is rated `intricate`; a `routine` task
takes the agent's own model.

**Never two at once.** Two implementers write into one working tree, and the second reads a
tree the first is still changing. Their diffs then cannot be reviewed apart, which is the thing
that makes each one reviewable at all.

**Review what comes back.** You get a report and nothing else — the implementer's transcript
never enters your context, which is the point. So read the actual diff:

```bash
git diff -- <the task's files>
git status --porcelain          # anything outside the boundary?
```

Then run the verification command yourself. A report saying it passed is a claim, and the whole
arrangement is worth nothing if the claim is what you act on.

**Decide.** Three outcomes:

- The diff does what the task said, stays inside its files, and verification passes → commit it,
  conventionally, one commit for the task.
- Something is wrong, missing, or outside the boundary → send it back. Dispatch a fresh
  implementer for the same task with what was wrong. Do not fix it yourself; you are holding the
  list, and a coordinator that starts writing code is a coordinator that stops reviewing it.
- Reported `blocked`, or sent back twice without converging → **stop the whole run** and report.
  A task that will not land is not a reason to keep going; the tasks after it were ordered
  behind it for a reason.

Record what each implementer `NOTED`. Out-of-boundary problems it found are yours to decide on
— a new task now, or something said in the pull request — and they are gone if you do not.

## 5 — Review the branch

Every task landed: invoke `review-implementation`.

If it comes back anything but clean — stalled, capped, no verdict — **stop**. Do not open a
pull request and do not mark the handoff consumed. The handoff staying where it is, is what
lets the work be picked up again rather than restarted.

## 6 — Ship it

Clean: invoke `open-pr`, with the handoff's PR title. That skill updates the documentation,
stages what the work taught into the memory store, pushes, opens the pull request and puts it
through a posted review.

## 7 — Close the handoff out

Only now, and only on a clean run:

```bash
mkdir -p handoff/done
mv "<the handoff you read>" handoff/done/
```

`handoff/` is excluded from git, so this is a plain move and stages nothing.

Then, if the plan names a slice after this one: invoke `write-handoff` for it, with the pull
request just opened as its **predecessor**. The next session will wait for it to merge.

Report the pull request, the posted review, and either the next handoff's path or that the plan
is complete.

## What this skill never does

It never merges, and never approves.

It never marks a handoff consumed on a run that did not reach a pull request. Everything about
this flow's recovery depends on that file staying where it is until the work it describes has
actually landed.
