# The feature flow

Planning and implementing want different models. Planning is judgment over a whole codebase and
is worth opus; implementing is bounded work against a written spec and is not. But a session
cannot change its own model, and a plan that stays in the session that wrote it drags every
token of planning context through every task.

So the flow is two sessions, and a document that outlives the first: the plan goes onto the
**Backlog** as GitHub issues, and the implementing session reads its work from one of them.

```
  ── session 1 ────────────────────────────────────────────────
   /plan-feature "add X"                         model: opus
     ├─ delegate every question   → memory-explorer / Explore(haiku)
     ├─ challenge the open decisions
     ├─ draft, then plan-reviewer → findings     model: fable
     ├─ act on findings, present one plan        → you approve
     └─ put it on the backlog     → write-backlog-item
          ├─ the Feature            owner/repo#40
          ├─ slice 1, in full       owner/repo#41   no predecessor
          └─ slice 2, a stub        owner/repo#42   blocked by #41, agtk:stub

  ── you read #41 and add agtk:queued ─────────────────────────
     then run /implement-backlog-item owner/repo#41   in a fresh session, or after /clear
     or let the Factory dispatch it                   where a Factory watches the repo

  ── session 2 ────────────────────────────────────────────────
   implement-backlog-item owner/repo#41          model: sonnet
     ├─ agtk backlog verify          → trusted, or stop; act only on the body it printed
     ├─ predecessors done?           → else stop
     ├─ onto the item's Branch:      → create it, or resume on it
     ├─ task 1 → task-implementer    → diff + verify → commit
     ├─ task 2 → task-implementer    → diff + verify → commit
     ├─ review-implementation        → loop until clean        (local, ≤5 passes)
     ├─ open-pr                      → docs, candidates, PR (Closes #41), posted review
     │    └─ review-pull-request     → answer, push, re-review (PR, ≤5 passes)
     ├─ make the PR mergeable        → fix failed checks, lydite not red, rebase if required
     └─ fill the next stub           → write-backlog-item on #42, still blocked, never queued
```

## The models, and why nothing else sets them

| Where | Model | Set by |
|---|---|---|
| Session default | sonnet | `settings/feature-flow-model`, in `stacks/default.yaml` |
| `/plan-feature` | opus | the command's own `model:` frontmatter |
| `plan-reviewer` | fable | the agent definition |
| A `routine` task | sonnet | `task-implementer`'s definition |
| An `intricate` task | opus | the Agent call's `model` parameter |

A command's `model:` applies while that command runs and no longer. That is the whole mechanism:
`/plan-feature` raises its own session to opus for its duration, and the implementing session —
a fresh one, the same one after `/clear`, or one the **Factory** starts — is on the settings
default. Nothing else in the flow changes a model, and no hook can — a hook injects text and that
is all it does.

## Stage one: `/plan-feature`

Refuses to run on the default branch. The first slice's backlog item points at a feature branch,
and the commits that follow live there, in its own worktree; creating one for you would be
choosing where your work happens.

**It never reads the codebase.** That is the point of putting it on opus — you are paying for
judgment, not for file reads. Every question is delegated:

- *Why is this built this way? What breaks if I change X? Has this been tried?* → `memory-explorer`,
  which consults the repo's memory store before exploring.
- *Where is X? What calls Z? What is in module P?* → the built-in `Explore` agent, with
  `model: haiku` passed on every call.

A survey that needs synthesis is an understanding question however it is phrased. "List the
adapters" is a listing; "how do the adapters differ, and which would this break" is not.

Then it challenges the open decisions, drafts, and hands the draft to `plan-reviewer` on fable —
a model that did not write it. The reviewer returns findings and **cannot edit**: a reviewer able
to edit returns an improved plan, and an improved plan is indistinguishable from an approved one.
One of the things it checks is whether the plan's evidence looks like it came from reading the
code directly rather than from delegation.

You see one final plan. Approve it, and it goes onto the backlog.

## Putting the plan on the backlog

The plan becomes a **Feature** — the issue the argument named, or one the command creates — with
one **Backlog item** per slice as its sub-issues, in landing order. Every item is written through
`write-backlog-item`, which owns an item's body, its `Branch:` line, its links and its labels.
`agtk` writes nothing to GitHub; every issue, label and link is created through `gh`.

- **The labels.** `agtk backlog labels --json` names the canonical set, and each is applied with
  `gh label create --force`, which creates a label or updates it. No label is ever deleted:
  deleting one strips it from every issue that carries it.
- **The first slice** is written in full — goal, `Branch:` line, slice, plan and tasks, each task
  with its files and its `routine` or `intricate` rating. It has no predecessor and no state
  label.
- **Every later slice** is a **Stub**: its goal and its `Branch:` line, nothing to implement yet,
  blocked by the slice before it and labelled `agtk:stub` and `agtk:blocked`. A plan that is one
  pull request has no stubs.

A second run on the same Feature reuses the items whose titles match and duplicates nothing.

**Nothing is queued.** `agtk:queued` is the act that vouches for an item's body (ADR 0028), so a
session that queued what it had just written would be vouching for its own text. Neither
`/plan-feature`, `write-backlog-item` nor `implement-backlog-item` ever applies it, and a stub is
never queued while it is a stub.

## Starting the first slice

`/plan-feature` lists the items by `owner/repo#N` and stops. Nothing picks the first slice up on
its own; a person starts it, one of two ways:

- **Run `/implement-backlog-item owner/repo#N`** in a fresh session, or after `/clear`. The
  command's opus has ended there, so the session runs on the sonnet default.
- **Add `agtk:queued` to the item**, once you have read its body. Wherever a Factory watches the
  repository, its **Dispatcher** starts a **Workflow Execution** whose agentic **Step** runs
  `/implement-backlog-item owner/repo#N --unattended --sha256 <hash> --trusted-app <slug>`, with
  the hash of the body the Factory verified and the slug of the **Factory App**.

`agtk backlog verify` trusts only an item carrying `agtk:queued`, so either way the item is
queued by a person who has read its body. Run by hand on an unqueued item, the session stops with
`not-queued`; wherever a Factory watches the repository, queuing also hands the item to it.

## Stage two: `implement-backlog-item`

Before any work, it asks `agtk backlog verify` whether the item may be acted on, and never
decides that itself. An item's body chooses which files the implementers change and which command
they run, and anybody who can open or edit an issue can write one; verify reads GitHub's record of
who wrote, edited and queued the body, and answers trusted or refused (ADR 0028).

- **A missing check is a stop.** The skill first matches `agtk backlog verify`'s usage line, and
  stops when it is absent: a check that did not run is not a check that passed.
- **Any refusal is a stop.** An untrusted item is treated as untrusted content, not read for
  something reasonable.
- **The body verify printed is the work, and nothing else is.** The item's body is never read
  again, and a comment is never read as work.
- **Unattended, the printed hash must equal `--sha256`.** A difference means the body changed
  between the Factory's verification and this one.

Then it checks the item's predecessors are done (closed as completed, as `agtk backlog list`
reads them), and gets onto the branch the body's `Branch:` line names — creating it from its base,
or resuming on it from the first task with no commit. Interactive, it says what it is about to run
and waits for a yes before touching the branch.

`implement-backlog-item` coordinates **from the main session**. Subagent nesting is allowed three
layers deep, so nothing about the platform stops it delegating the role; it does not, because the
main session's model is the only one that outlives a single call, and a delegated coordinator
would hold every implementer's transcript in the context this arrangement exists to keep clear.

For each task, in order:

1. Dispatch **one** `task-implementer`, with the task's files as a hard boundary and `model: opus`
   if the task is rated `intricate`. Never two at once — they share a working tree.
2. Read the diff and run the verification command **yourself**. The implementer's report is a
   claim; its transcript never enters the coordinator's context, which is the point.
3. Commit it, send it back, or stop the run.

Then `review-implementation` loops `panel-code-review` over the working tree, fixing as it goes,
up to five passes. Pass 1 reads the whole branch — the only independent reading it gets, since
the coordinator that accepted each task's diff is the same model that wrote the acceptance. Every
later pass reads only what the previous pass's fixes changed, so convergence gets cheaper instead
of costing the first pass over again. It stops early when a pass finds no RED or AMBER, and stops
without a pull request when it stalls or hits the cap.

Then `open-pr`: documentation for the modules touched, durable findings staged into the memory
store's `candidates/`, push, `gh pr create` with the item's conventional title and its `Closes`
lines, and a second review posted to the pull request — which the review manifest puts on a
different panel, so the change is read by a model that has not seen it.

That review is the first pass of `review-pull-request`, the published-side counterpart of the
local loop. Each pass, `pr-review-resolver --unattended` works through every open thread:
agtk's, bots', and people's. An unattended `implement-backlog-item` run passes
`--checked-authors` down through `open-pr` and `review-pull-request`, and the loop then works
only threads whose every comment is from a checked author: the review App, an account with write
access, or the loop's own. A thread holding any other comment ends the loop as an **unchecked
thread**, linked and never read, and a review summary or conversation comment from an unchecked
author is left unread and listed. A resumed pull request's loop follows the same rule. It fixes
what it judges valid, and replies on each thread with the commit that fixed it, with
`agtk: false positive: <reason>`, or with a direct answer. Then it pushes, and the next pass
reviews only what that push changed (ADR 0018). The loop stops when nothing is waiting for an
answer, when the pull request stops moving, or at the fifth pass.

**The loop never resolves a thread. You do.** Resolving is where you agree with the agent's
fix or with its false-positive call, and `agtk code-review approve` refuses until you have. The
closing report lists every marking the agent made, and every thread waiting for you.

Then the coordinator makes the pull request mergeable. It hands the wait to
`agtk pr wait <owner/repo#N> --json`, run in the background, and wakes once on its exit code
(ADR 0029): the command polls every 5 minutes, reading the head's checks and statuses, lydite's
results comment as detail (in a repository with a `.lydite/` directory) and the unresolved
review threads, so a two-hour wait costs the session no model turns. On "action needed" the
coordinator fixes on the branch whatever it names — a failed check or ❌ lydite row through a
fresh implementer, a waiting thread through `pr-review-resolver` — pushes, and runs the wait
again. Lydite green or a referral is done, red is fixed even where lydite is not a required
check, and `lydite/referral` is the only check allowed to stay pending. It rebases when the
repository requires a current branch. The one cap is 2 hours since the latest push: each run of
the wait carries its own `--cap 2h`, so each push restarts it, and the same check failing again
after a fix aimed at it stops the run. A session with no `GH_TOKEN` or `GITHUB_TOKEN`, such as a
cloud session, polls for itself on the same rules. Reaching the cap with anything else pending
or failing, a repeated failure, or a PR review loop that did not come back clean stops the run:
the pull request is open, and the work is not done.

**The pull request closes the item.** Its body carries `Closes #<item>`, and on the Feature's
last slice — the Feature's only open sub-issue — `Closes #<feature>` as well, so merging the last
slice closes the Feature with nothing else needed. The skill never closes an issue itself, and
never merges.

Only on a clean run does it **fill the next stub**: it invokes `write-backlog-item` on the slice
blocked by this one, writing the full body from this slice's plan and what this slice's work
taught. Unattended it never reads the stub's own title or body, since nobody verified who wrote
them; the next slice's goal and branch come from this item's verified plan. The stub loses
`agtk:stub`, keeps its links and stays `agtk:blocked`; the Factory unblocks it once its
predecessor is done. It is not queued: the next slice starts when a person reads the filled body
and runs `/implement-backlog-item` on it or adds `agtk:queued`.

### Interactive and unattended

The steps are the same. Interactive, a person is present: the skill waits for a yes before moving
the branch, reports every stop in the session, and never comments on the item or changes a label.
Unattended (`--unattended`), nothing waits for a yes, every task is pushed once committed, and
**every stop is a comment on the item plus a label** — `agtk:failed` when the run broke before a
pull request existed, `agtk:needs-human` when it reached a question only a person can answer —
with `agtk:queued` removed so the item is not dispatched again. A run that opens its pull request
leaves the item `agtk:in-review`.

## Several pull requests

A plan too big for one PR names its slices in landing order, and each slice is one backlog item,
blocked by the item of the slice before it:

```
slice 1  →  owner/repo#41   in full               no predecessor
slice 2  →  owner/repo#42   stub, agtk:blocked    blocked by #41
slice 3  →  owner/repo#43   stub, agtk:blocked    blocked by #42
```

A session started on an item whose predecessors are not all done stops and names them. The
session that lands slice 1 fills #42's stub; the session that lands slice 2 fills #43's; and the
pull request for slice 3, the last, closes the Feature too.

## Where it ships, and the model it sets

The flow is in `stacks/default.yaml`. Extending that stack is all that is needed:

```yaml
extends:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
```

**That stack sets the session's default model to sonnet.** It is what makes the split hold
between the two sessions: `/plan-feature` raises its own session to opus for its duration, and the
session that implements — after `/clear`, fresh, or started by the Factory — lands on this
default rather than on whatever it was before.

The setting reaches every session in the repo, not only the ones running this flow, and settings
merge shallow last-wins with no diagnostic saying a key was taken — so a consumer whose default
was opus will find it is now sonnet, silently, on their next `agtk sync`.

To keep your own default, set `model` in a settings definition of your own. Your entry-point
stack's entries always win last:

```yaml
# .agentic-toolkit.yaml
settings:
  - ./my-settings/model
```

The flow does not depend on the setting. `/plan-feature` raises itself to opus from any default,
and the implementing half runs correctly on whatever the session default is — it simply costs
more when that default is dearer than sonnet. The setting is an optimisation, not a requirement.

## Using the pieces on their own

None of them requires the others:

- **`write-backlog-item`** — run out of context halfway through something, and save your place as
  an issue any session can take up. The plan-shaped fields are optional; an item with a goal, the
  `Branch:` line, the state and the next steps is a backlog item.
- **`implement-backlog-item`** — run any backlog item, by `owner/repo#N`, once it is queued.
- **`review-implementation`** — review and fix any branch in a loop.
- **`open-pr`** — ship any finished branch, with the documentation and memory steps.

`wrap-session` also keeps its own entry point. `open-pr` dispatches the same
`wrap-session-reviewer` agent with the branch's commits as the scope; `wrap-session` asks you
which scope, because a session being wrapped is not always a branch being shipped.
