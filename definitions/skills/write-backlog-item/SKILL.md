---
name: write-backlog-item
description: |
  Write or update one backlog item — the GitHub issue a session with no memory of this one reads to carry the work forward: the
  goal, the source branch, the state, the next steps, and optionally a plan's tasks. Owns the backlog item's body format, its links to
  the Feature and its predecessors, and the labels it is created with. Written by /plan-feature after approval, when a slice's
  stub is filled, and by hand when a session runs out of context mid-work. Never queues anything. Trigger on "write a backlog
  item", "fill the stub", "save my place as an issue", "put this on the backlog", "continue this from an issue".
extensions:
  claude:
    argument_hint: "[owner/repo#N to update] [what the item must accomplish]"
---

# Write backlog item

Write the GitHub issue a session with no memory of this one reads to continue the work.

A backlog item is a goal, the current state and the next steps, and optionally a plan's tasks,
carried on a GitHub issue. Its terms are in
`CONTEXT.md`: **Backlog item**, **Stub**, **Feature**, **Factory**, **Dispatcher**, **Factory
App**. The task list and the slice are **optional**: a mid-work save carries neither and is still
a backlog item.

Read `references/backlog-item-template.md` before writing. It is the body's shape; this file is
the rules.

An item has four parts, and each lives in exactly one place:

- **Title** — the slice's one-line goal.
- **Body** — the template's sections, with the required `Source:` line naming the branch the work
  starts from and the pull request targets. It never names the branch the work happens on: that is
  the implementing session's to choose, and it records the choice in a progress comment of its
  own. The body is the work and nothing else is: comments on the issue are never read as work
  (ADR-0028), so nothing this skill writes goes into a comment, bar a mid-work save's progress comment (step 7), which
  is state and not work.
- **Links** — each predecessor is a blocked-by link (GitHub's issue dependencies), and the Feature
  is the item's sub-issue parent. Neither is written into the body.
- **Labels** — at most one state label from the canonical set, plus `agtk:stub` while the item is
  a stub.

`agtk` writes nothing to GitHub (ADR-0019, ADR-0028): `agtk backlog` only reads. Every issue,
label and link here is created by this skill through `gh`, and every `gh` call it makes is written
out below. When no authenticated `gh` is reachable, stop and say so; do not route the write
through anything else.

## 1 — Establish the goal and the repository

The goal comes from the argument if one was given, from the conversation if it is unambiguous,
and otherwise from asking. The goal is the filter that decides what goes in and what is
deliberately left out; an item written without one carries everything, which is the same as
carrying nothing.

The repository is the one the argument names, otherwise this checkout's:

```bash
repo="$(gh repo view --json nameWithOwner --jq .nameWithOwner)"
```

When the argument names an existing item (`owner/repo#N`), this run edits it; otherwise it creates
one.

## 2 — Leave the worktree recoverable

An item must never point at work that only exists in a context about to be discarded.

```bash
git status --porcelain                  # must print nothing
git fetch origin
git rev-parse --abbrev-ref @{upstream}  # must name the branch's remote; fails when there is none
git rev-list --count @{upstream}..HEAD  # must print 0
```

- Uncommitted or unstaged changes: commit them, or say in the body's **State** exactly why they
  are being left and what they are.
- A branch with no upstream, or with commits the upstream lacks: push it. A branch that exists only
  locally is one machine away from being the only copy, and the session that takes the item may
  run on another machine entirely.

If you cannot reach that state — mid-refactor, nothing committable — **stop and say what is
blocking it**. Do not write an item that points at a dirty or unpushed worktree.

## 3 — Write the contents

Follow the template. Carry only what the goal needs, and do not duplicate what a plan, an ADR, an
issue or a commit already records — reference it by path or URL. The one thing worth repeating is
reasoning that exists nowhere but this conversation.

The `Source:` line is required on every item, a stub included: the session that takes the item
reads the branch to start from off the body and from nowhere else.

**Never name the work branch.** The branch an item's work happens on belongs to
`implement-backlog-item`, which derives a name of its own from the item's number and title, creates
it from `origin/<source>` in its own worktree and records it in the progress comment. A body that
named it would be a second record of something the progress comment already holds, and a name
chosen before the work starts is a name a later slice could reuse after an earlier slice's pull
request merged. The `Source:` of every slice is the branch the repository merges into, never an
earlier slice's work branch.

Rate each task `routine` or `intricate`. The rating picks the model that implements it, so rate by
how much judgment the task needs, not by how many lines it touches: a wide mechanical rename is
`routine`, and a small change to something with an invariant nobody wrote down is `intricate`.

Name the files each task may touch. That list is the implementer's boundary, so a task whose files
cannot be named yet is a task that has not been thought through — split it or say so.

Write the body to a temporary file and pass it with `--body-file`. Never pass a body inline with
`--body`: a body carries backticks, `$` and quotes that the shell rewrites before `gh` sees them.

```bash
body="$(mktemp)"
# write the body into "$body", following the template
```

## 4 — Create or edit the issue

### A new item

The labels have to exist on the repository before an issue can carry them. `/plan-feature`
creates them; when one is missing, create the canonical set the same way, which creates a label or
updates it and never deletes one:

```bash
agtk backlog labels --json \
  | jq -r '.[] | [.name, .color, .description] | @tsv' \
  | while IFS=$'\t' read -r name color description; do
      gh label create "$name" --repo "$repo" --color "$color" --description "$description" --force
    done
```

Never delete a label: deleting one strips it from every issue that carries it.

Create the item, with `--label agtk:blocked` only when step 6 says it is blocked and
`--label agtk:stub` only when it is a stub:

```bash
url="$(gh issue create --repo "$repo" --title "<the slice's one-line goal>" --body-file "$body")"
number="${url##*/}"
```

```bash
# a stub blocked by the slice before it
url="$(gh issue create --repo "$repo" --title "<the slice's one-line goal>" --body-file "$body" \
  --label agtk:stub --label agtk:blocked)"
number="${url##*/}"
```

### An existing item

Replace the body and change the title only when the goal changed:

```bash
gh issue edit "$number" --repo "$repo" --body-file "$body"
gh issue edit "$number" --repo "$repo" --title "<the slice's one-line goal>"
```

Leave its state label alone: an item's state after it exists is moved by a person or by the
Factory, not by this skill.

Editing the body of an item that carries `agtk:queued` makes `agtk backlog verify` refuse it,
because an edit later than the queuing means nobody trusted queued the body as it now stands
(ADR-0028). Do not re-apply the label to clear that. Tell the user the item has to be re-queued by
a person who has read the new body.

```bash
rm -f "$body"
```

## 5 — Link the Feature and the predecessors

Both links take the numeric issue **id**, not the issue number. Read it from the issue:

```bash
id() { gh api "repos/$repo/issues/$1" --jq .id; }
```

**The Feature.** A planned item is a sub-issue of its Feature. Check whether it already is, and
link it only when it is not — a second link to the same parent is refused:

```bash
gh api --paginate "repos/$repo/issues/$feature/sub_issues" --jq '.[].number' | grep -qxF "$number" \
  || gh api --method POST "repos/$repo/issues/$feature/sub_issues" -F sub_issue_id="$(id "$number")"
```

**The predecessors.** Each is a blocked-by link from this item to the issue it waits on. Read the
existing ones first and add only the missing — a second link to the same issue is refused. Run
this once per predecessor:

```bash
gh api --paginate "repos/$repo/issues/$number/dependencies/blocked_by" --jq '.[].number' | grep -qxF "$predecessor" \
  || gh api --method POST "repos/$repo/issues/$number/dependencies/blocked_by" -F issue_id="$(id "$predecessor")"
```

When the plan spans several slices, each slice's item is blocked by the item of the slice before
it. The first slice has none.

This skill never removes a link. A predecessor or a Feature that is wrong is a decision for the
user, not a side effect of rewriting a body.

## 6 — Label it, and never queue it

A new item is `agtk:blocked` when any of its predecessors is not done. Done is what `agtk backlog
list` reads it as: closed as completed. An open predecessor, or one closed as not planned, leaves
the item blocked. Check each one:

```bash
gh issue view "$predecessor" --repo "$repo" --json state,stateReason --jq '.state + " " + .stateReason'
# done only when it prints: CLOSED COMPLETED
```

A new item with every predecessor done, or with none, carries no state label: it is left
unqueued.

**This skill never applies `agtk:queued`**, to a new item, to an edited one, or to a filled stub,
and never says it did. Queuing is the act that vouches for the body (ADR-0028): a person with write
access applies it after reading the item, or the Factory does through the Factory App. A skill that
queued what it had just written would be vouching for its own text.

It never applies `agtk:running`, `agtk:in-review`, `agtk:failed` or `agtk:needs-human` either.
Those belong to the session running the item and to the Factory.

## 7 — The three shapes

### A planned slice

Written by `/plan-feature` after approval, or by hand for a slice of a Feature. The item is a
sub-issue of the Feature, blocked by the slice before it, and its `Source:` is the branch it starts
from and its pull request targets. A later slice that holds only a goal is created as a **stub**:
its body is the **Goal** section and the `Source:` line, and it carries `agtk:stub` beside
`agtk:blocked`. A stub is never queued while it is a stub.

### Filling a stub

Done by the session that lands the slice before it, once there is something to implement.

1. Replace the body with the full one: `gh issue edit "$number" --repo "$repo" --body-file
   "$body"`.
2. Drop the stub label: `gh issue edit "$number" --repo "$repo" --remove-label agtk:stub`.
3. Keep the predecessor links and the Feature link exactly as they are. Do not remove them and do
   not re-create them; step 5's checks find them present and add nothing.
4. Leave `agtk:blocked` in place. The Factory unblocks an item once its predecessors are done.

### A mid-work save

Written by hand when a session runs out of context halfway through something. The item has no
Feature and no predecessors, its `Source:` is the branch the work was created from. The work so
far lives on the current branch, pushed, which the body never names. Instead the save also posts
the item's **progress comment** — the one comment this skill writes — so that
`implement-backlog-item` takes the restart path onto that branch rather than starting a new one:

```bash
based="$(git merge-base HEAD "origin/<source>")"
# once the issue exists and its number is known:
branch="feat/backlog-${number}-<a word or two of the goal>"
git branch -m "$branch" && git push -u origin "$branch"
```

The comment is in the shape `implement-backlog-item`'s step 4 defines, beginning with its marker
line: `Branch:` is the renamed `$branch`, `Source` and `Last checked` are `$based`, and the table has
one row per task. A task the branch already carries is `done` with its commits, or `rework` when
it is only partly done; a task with no commits is `pending`. It has **no `Body` line**, so the first run reconciles the work against the body as it
stands. Post it as `implement-backlog-item` step 5 does, from a file with `-F body=@`. The restart
refuses a branch outside `feat/backlog-<number>-<slug>`, so the save renames and pushes the branch
as above, and only then posts the comment. The branch's old name stays on the remote, deliberately: this skill never deletes a
branch, and a person removes it once the item no longer needs it. A branch that cannot be renamed and pushed is left out of the comment, and **State** says
the work is not resumable by a session. The item carries no label: it is unqueued, and `agtk backlog list` does not show it
until it carries one.

## 8 — Say what happens next

Output the item's URL and its `owner/repo#N`.

You cannot start the next session yourself, and you never apply `agtk:queued`. Tell the user the
item starts once a person with write access has read its body and added `agtk:queued`; queuing is
what vouches for the body. After that:

- they run `/implement-backlog-item owner/repo#N` in a session themselves, or
- where a Factory watches the repository, it dispatches the item.

Never say the item is picked up automatically: nothing runs it until a person has queued it and
then runs it or leaves it to a Factory. A stub has no way in until it is filled.
