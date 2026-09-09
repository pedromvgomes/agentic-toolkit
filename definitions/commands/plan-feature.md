---
description: Plan a feature on opus without reading the codebase — delegate every question, challenge the open decisions, have the draft reviewed, then write the handoff the implementing session picks up.
model: opus
argument_hint: "<what to build, or an issue reference>"
tools: [Agent, Task, Skill, Bash, TodoWrite, AskUserQuestion, ExitPlanMode]
requires:
  - agents/plan-reviewer
  # The two explorers section 1 routes to. Without either, every question it
  # forbids answering directly has nowhere else to go.
  - agents/memory-explorer
  - skills/challenge
  - skills/write-handoff
---

Plan the work described by: $ARGUMENTS

You are on opus. That is the expensive part of this flow and the only part that should be, so
spend it on judgment and spend nothing on reading.

## 0 — Refuse to plan in the wrong place

Check the branch:

```bash
git rev-parse --abbrev-ref HEAD
```

If it is the repository's default branch, **stop**. Say that this needs a feature branch in its
own worktree, and that the branch is where the resulting handoff and its commits will live. Do
not create one — where somebody's work happens is theirs to choose.

## 1 — Never read the codebase yourself

You do not open source files, and you do not grep. Every question about the code goes to a
subagent, and which one depends on the kind of question:

| Question | Goes to |
|---|---|
| Why is this built this way? What breaks if I change X? What constrains this? Has this been tried? | `memory-explorer` |
| Where is X? What calls Z? What is in module P? | `Explore`, with `model: haiku` |

Pass `model: haiku` on **every** `Explore` call. Its default is not haiku, and a survey run on
anything dearer is the cost this arrangement exists to avoid.

**A survey that needs synthesis is an understanding question.** "List the adapters" is a
listing; "how do the adapters differ, and which would this change break" is not, whatever it
looks like. Route the second to `memory-explorer` — it consults the memory store first, and the
answer is one somebody has often paid for already.

Do not create another explorer. These two cover it, and a third would need a rule for when it
wins.

`Bash` is here for `git` and `gh`, not for reading source. `cat`, `sed -n`, `head` and `rg` over
the project's files are the thing this section forbids, whichever tool reaches them.

## 2 — Challenge the open decisions

If the request carries decisions that are not settled — trade-offs, ambiguity, unstated
assumptions — invoke the `challenge` skill and work through them before drafting. A plan whose
decisions were never surfaced is a plan somebody has to relitigate while implementing it, on a
model chosen for implementing.

## 3 — Draft

The plan says what is being built, in what order, and how each piece is verified.

It must state **whether this is one pull request or several**. If several, name the slices in
landing order, and make each one landable alone: it merges, CI passes, and nothing it leaves
behind is broken while the next slice is unwritten.

Break the first slice into tasks. Each task carries the files it may touch and a rating —
`routine` or `intricate` — which chooses the model that implements it. Rate by how much
judgment the task needs, not by how many lines it touches.

## 4 — Have it reviewed

Dispatch the `plan-reviewer` agent with the draft.

It returns findings, not a rewritten plan. Among other things it checks whether the plan's
evidence looks like it came from reading the code here rather than from delegation — which is
the rule in section 1, checked by something that did not write the plan.

## 5 — Act on the findings, then present once

Decide on each finding. Fix what holds; say what you are not taking and why. A finding you
disagree with is a fine outcome — a finding you quietly dropped is not.

Present **one** final plan to the user for approval, with the decisions explicit. Not the draft,
not the findings, and not a diff between them.

## 6 — Hand off

Once the user approves, invoke the `write-handoff` skill for the **first slice**.

Then tell the user to run `/clear`, and stop.

That is the whole handover. Nothing else in this flow changes the model: your `model: opus`
applies while this command runs and no longer, so the session that comes back after `/clear` is
on the settings default — which is what the implementing half is meant to run on. The
session-start hook finds the handoff and says what to invoke.

Do not start implementing. Not the first task, not the easy one, not a stub. The implementing
session exists because doing that here is what this flow is arranged to prevent.
