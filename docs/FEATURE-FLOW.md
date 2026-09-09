# The feature flow

Planning and implementing want different models. Planning is judgment over a whole codebase and
is worth opus; implementing is bounded work against a written spec and is not. But a session
cannot change its own model, and a plan that stays in the session that wrote it drags every
token of planning context through every task.

So the flow is two sessions with a `/clear` between them, and a document that survives it.

```
  ── session 1 ────────────────────────────────────────────────
   /plan-feature "add X"                         model: opus
     ├─ delegate every question   → memory-explorer / Explore(haiku)
     ├─ challenge the open decisions
     ├─ draft, then plan-reviewer → findings     model: fable
     ├─ act on findings, present one plan        → you approve
     └─ write-handoff             → handoff/20260909-1412-add-x.md

  ── you run /clear ───────────────────────────────────────────

  ── session 2 ────────────────────────────────────────────────
   SessionStart hook sees handoff/*.md           model: sonnet
     └─ "invoke implement-handoff"
          ├─ predecessor merged?    → else stop
          ├─ task 1 → task-implementer  → diff + verify → commit
          ├─ task 2 → task-implementer  → diff + verify → commit
          ├─ review-implementation      → loop until clean
          ├─ open-pr                    → docs, candidates, PR, posted review
          ├─ handoff → handoff/done/
          └─ next slice? → write-handoff, predecessor = the PR just opened
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
`/plan-feature` raises its own session to opus for its duration, and the session that comes back
after `/clear` is on the settings default. Nothing else in the flow changes a model, and no hook
can — a hook injects text and that is all it does.

## Stage one: `/plan-feature`

Refuses to run on the default branch. The handoff and the commits that follow live on a feature
branch in its own worktree, and creating one for you would be choosing where your work happens.

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

You see one final plan. Approve it, and `write-handoff` writes the first slice.

## The `/clear`, which you do by hand

Nothing in the flow can clear a session's context, and nothing should pretend to. When
`/plan-feature` finishes it tells you to run `/clear`, and stops.

## Stage two: the handoff

A `SessionStart` hook fires on `startup` and `clear` — the two ways a session begins with no
memory of the one before it — globs `handoff/*.md` at depth one, and injects an instruction to
invoke `implement-handoff`. It cannot run the skill and cannot change the model. It only says
what is waiting.

`implement-handoff` coordinates **from the main session**. Subagent nesting is allowed three
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
store's `candidates/`, push, `gh pr create` with the handoff's conventional title, and a second
review posted to the pull request — which the review manifest puts on a different panel, so the
change is read by a model that has not seen it.

## Several pull requests

A plan too big for one PR names its slices in landing order. Each slice becomes its own handoff,
and every slice after the first records the one before it as its **predecessor**:

```
slice 1  →  PR #91                          no predecessor
slice 2  →  handoff written when #91 opened  predecessor: #91
slice 3  →  handoff written when #92 opened  predecessor: #92
```

When a session picks up a handoff with a predecessor, it checks that pull request is **merged
into the base branch** before doing anything. Not merged — open, closed, draft — it stops and
tells you which one is pending.

Merged, and the branch has to move onto the updated base. The next session continues in the
**same worktree**; there is no "start a new worktree" step. It states the sequence and waits for
your yes before running it, because that is the one step that changes where you are standing:

```bash
git status --porcelain          # must be empty
git fetch origin
git checkout main && git merge --ff-only origin/main
git checkout -b <the branch the handoff names>
```

## `handoff/` is never committed

The folder holds documents about how somebody is working, not about the project, so it is
excluded per checkout rather than written into your `.gitignore`:

```bash
echo 'handoff/' >> "$(git rev-parse --git-common-dir)/info/exclude"
```

`--git-common-dir` rather than `--git-dir`: under a `gt` bare repo it resolves to the shared
`.bare/`, so one write covers every worktree. `write-handoff` does this itself, idempotently.

A consumed handoff moves to `handoff/done/`, which the hook's depth-one glob does not see.

**The exclude is a convenience, not the control.** A git exclude has no effect on a file that is
already tracked, so a branch can commit `handoff/anything.md` regardless — and a handoff decides
what the next session does, which subagents it dispatches and what command it runs. Both the hook
and `implement-handoff` therefore check the thing that cannot be forged from inside a branch:

```bash
git ls-files --error-unmatch -- handoff/x.md    # tracked → refuse
```

A tracked handoff is never advertised and never acted on. Its presence is reported, because a
committed one is a fact worth knowing rather than a file to skip quietly.

## Where it ships, and the model it sets

The flow is in `stacks/default.yaml`. Extending that stack is all that is needed:

```yaml
extends:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
```

**That stack sets the session's default model to sonnet.** It is what makes the split hold across
the `/clear`: `/plan-feature` raises its own session to opus for its duration, and the session
that comes back lands on this default rather than on whatever it was before.

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

- **`write-handoff`** — run out of context halfway through something. The plan-shaped fields are
  optional; a handoff with a goal, the state and the next steps is a handoff.
- **`review-implementation`** — review and fix any branch in a loop.
- **`open-pr`** — ship any finished branch, with the documentation and memory steps.
- **`implement-handoff`** — run a handoff you wrote by hand.

`wrap-session` also keeps its own entry point. `open-pr` dispatches the same
`wrap-session-reviewer` agent with the branch's commits as the scope; `wrap-session` asks you
which scope, because a session being wrapped is not always a branch being shipped.
