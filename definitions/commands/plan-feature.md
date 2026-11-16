---
description: Plan a feature on opus without reading the codebase — delegate every question, challenge the open decisions, have the draft reviewed, then put the approved plan on the backlog as a Feature with one backlog item per slice.
model: opus
argument_hint: "<what to build, or an issue reference>"
tools: [Agent, Task, Skill, Bash, TodoWrite, AskUserQuestion, ExitPlanMode]
requires:
  - agents/plan-reviewer
  # The two explorers section 1 routes to. Without either, every question it
  # forbids answering directly has nowhere else to go.
  - agents/memory-explorer
  - skills/challenge
  - skills/write-backlog-item
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
own worktree, and that the branch is where the first slice's backlog item points and its commits
will live. Do not create one — where somebody's work happens is theirs to choose.

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

## 6 — Put it on the backlog

Once the user approves, the plan becomes a **Feature** with one **Backlog item** per slice, its
sub-issues, in landing order. The terms are in `CONTEXT.md`. Every item is written through the
`write-backlog-item` skill, which owns an item's body, its links and its labels; this section
decides which items exist and in what order, and nothing more.

`agtk` writes nothing to GitHub: every issue, label and link below is created through `gh`. When no
authenticated `gh` is reachable, stop and say so.

### The repository and its labels

The repository is the one the argument's issue belongs to, otherwise this checkout's:

```bash
repo="$(gh repo view --json nameWithOwner --jq .nameWithOwner)"
```

Make sure the canonical labels exist on it. `agtk backlog labels --json` prints them as a JSON
array of `{"name","color","description"}`; apply each one with `--force`, which creates a label or
updates it in place:

```bash
agtk backlog labels --json \
  | jq -r '.[] | [.name, .color, .description] | @tsv' \
  | while IFS=$'\t' read -r name color description; do
      gh label create "$name" --repo "$repo" --color "$color" --description "$description" --force
    done
```

Never delete a label, and never remove one from an issue: deleting a label strips it from every
issue that carries it, and the state an item is in belongs to a person or the Factory.

### The Feature

If the argument named an issue (`#N`, `owner/repo#N`, or its URL), that issue is the Feature:

```bash
feature=<N>
```

Otherwise create one. Its title is the feature's one-line goal and its body is the approved plan in
brief — what is being built, the slices in landing order, and the decisions the plan settled.
Write the body to a temporary file and pass it with `--body-file`; a body passed inline with
`--body` is rewritten by the shell before `gh` sees it:

```bash
body="$(mktemp)"
# write the approved plan into "$body"
url="$(gh issue create --repo "$repo" --title "<the feature's one-line goal>" --body-file "$body")"
feature="${url##*/}"
rm -f "$body"
```

The Feature carries no state label and is never queued: it is never an item itself.

### What already exists

A second run of this command on the same Feature must not duplicate its items. Read the Feature's
sub-issues first:

```bash
gh api --paginate "repos/$repo/issues/$feature/sub_issues" \
  --jq '.[] | [.number, .state, .title] | @tsv'
```

A slice whose title — its one-line goal — matches a sub-issue's title exactly already has its
item: reuse that issue's number and create nothing for it. Pass it to `write-backlog-item` as
`owner/repo#N` only when what the plan now says of that slice differs from its body, so the skill
edits the item rather than creating another; its links are checked before they are added, so
nothing is linked twice. An existing item that already carries `agtk:queued`, `agtk:running` or
`agtk:in-review` is somebody's work in progress: leave it as it is, and tell the user what the
plan now says differently.

### The first slice

Invoke `write-backlog-item` for the **first slice**, in full: the goal, the `Branch:` line, the
**Slice** and **Plan** sections and the tasks from step 3, each with its files and rating. It is a
sub-issue of the Feature and has no predecessor, so it carries no state label: it is left
unqueued.

### Every later slice

Invoke `write-backlog-item` for each later slice, in landing order, in its **stub** shape: the
**Goal** section and the `Branch:` line, nothing to implement yet. Each one is

- a sub-issue of the Feature,
- blocked by the item of the slice before it, and
- labelled `agtk:stub` and `agtk:blocked`.

Its tasks are written by the session that lands the slice before it, which fills the stub once
there is something to implement. A plan that is one pull request has no later slices.

### Say what happens next

List every item by `owner/repo#N`, in landing order, with the Feature and which of them are stubs.

Then tell the user how the first slice starts — and that only a person starts it. The item starts
once a person with write access has read its body and added `agtk:queued`; queuing is what vouches
for the body. After that:

- they run `/implement-backlog-item owner/repo#N` in a session themselves, or
- where a Factory watches the repository, it dispatches the item.

Run `/implement-backlog-item` in a fresh session, or after `/clear`. Your `model: opus` applies
while this command runs and no longer, so that session is on the settings default — which is what
the implementing half is meant to run on.

**Never apply `agtk:queued` yourself**, to the first slice or any other. Queuing is the act that
vouches for an item's body, and a session that queued the items it had just written would be
vouching for its own text. Never say an item will be picked up on its own: nothing runs it until a
person has queued it and then runs it or leaves it to a Factory.

Do not start implementing. Not the first task, not the easy one, not a stub. The implementing
session exists because doing that here is what this flow is arranged to prevent.
