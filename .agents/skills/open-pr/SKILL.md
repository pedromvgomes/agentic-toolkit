---
name: open-pr
description: "Ship the current branch: update the documentation for the modules it touched, stage what the work taught into the memory store,\npush, open the pull request under the repo's git rules, and put it through a posted panel review. Usable on its own for any\nfinished branch, and the step `implement-handoff` forks to once its review loop comes back clean. Trigger on \"open a PR\",\n\"open the PR for this branch\", \"ship this branch\", \"raise a pull request\", \"let's get this reviewed\".\n"
---

# Open PR

Turn a finished branch into a pull request that has been documented, reviewed and recorded.

The order matters and is not negotiable: everything that changes the branch happens
**before the pull request exists**. Documentation written after `gh pr create` lands outside
the change it describes, and findings staged afterwards belong to a session that has already
ended.

## 0 — Refuse the obvious mistakes

Stop, and say which applies, if any of these hold:

- The current branch is the default branch. A pull request needs somewhere to come from.
- Nothing separates this branch from its base. There is no change to open.
- A pull request is already open for this branch. Say so, give its URL, and stop — re-reviewing
  an open PR is `panel-code-review`'s to do directly, and opening a second is not a thing to
  recover from.

Follow the repo's git rules for everything below: establish the layout, read the GitHub user
from the root `.envrc` before any `gh` call, and never merge.

## 1 — Documentation, before anything is pushed

Dispatch the `wrap-session-reviewer` subagent with:

- `scope`: `branch_commits` — the commits on this branch that are not on the default branch.
  This is the only scope that means anything here. Do not ask which scope to use; `wrap-session`
  asks because a session being wrapped might not be a branch being shipped, and this one is.
- `repo_root`: the absolute repository root.
- `session_summary`: one to three sentences on what this branch does, at the level of themes and
  modules touched.

Show what it reports, unedited. If it wrote nothing, say so and continue — a branch that
warrants no documentation change is an ordinary outcome, not a problem.

## 2 — Stage what the work taught, before the context is gone

```bash
agtk memory stats --json
```

If `agtk` is missing, the command exits non-zero, or the output carries no `root`, skip this
section entirely and say nothing about it. The repo has not adopted a memory store or cannot
reach one, and inventing a path loses the finding silently.

Otherwise go through what this branch cost you to learn, and stage the entries that are
**durable facts about the codebase** and **can name a file they came from**. Both, not either:

| Stage it | Leave it out |
|---|---|
| The hook's `fail_closed` parses and never reaches settings.json | This branch took three passes to converge |
| Stack self-sufficiency is enforced, so a `requires:` must also be listed | Rebase before pushing |
| Approach X was tried against `resolver.go` and reverted because Y | The tests are slow |

The right-hand column is true and is not a fact about the codebase. A note must carry at least
one anchor, so an entry with no file to point at cannot become one however useful it is —
staging it only buys a rejection later.

One file per finding, at `<root>/candidates/<YYYYMMDD>-<short-slug>.md`, carrying `about`, `saw`
and a body with a pointer for every claim. No `targets` or `verdict`: these are new findings,
not re-checks.

Then look at what is **already** in `candidates/`. A finding staged before this branch was
written was staged against code that has since moved, and a branch that renames or deletes what
one names leaves a finding that still reads as true. The curator is fed the candidates and the
matching slice of the index and **not the code**, so it has nothing to catch that with: a
candidate naming a symbol you just deleted reads to it exactly like one naming a symbol that is
still there. Promotion writes it into `notes/`, where a wrong claim is most expensive.

You are the last reader who can tell. Rewrite such a candidate to what now holds, or delete it
when the branch removed its subject outright, and say which you did and why. Leaving it is not
the neutral option — it is the choice that lets the false claim through.

This is not an exception to the rule below. `candidates/` is the staging area every producer
writes to, and it is not `notes/`.

**Never write, edit, stamp or delete a note**, and never run `agtk memory anchor` or
`agtk memory index`. `notes/` has exactly one writer and it runs from `/memory-curate`
(ADR 0003). Say in your output that candidates are waiting.

## 3 — Commit and push

Commit anything the steps above produced, as its own commit, conventional like the rest. Push
the branch and set upstream if it has none.

## 4 — Open it

The title is a conventional-commit subject, because squash merge makes it a subject on the
default branch and no commit message underneath can correct it. Take it from the argument if
one was given; otherwise derive it from the branch's commits.

The body says what the change does and why, and links whatever it closes. It carries no
co-authorship trailer and no link back to the session that produced it.

Report the URL.

## 5 — Review it, posted

Invoke `panel-code-review` on the pull request just opened.

This is a second reading, not a repeat of any local one: the review manifest's defaults put a
pull request on a different panel from a working tree, so the change is read by a model that
has not seen it. Report the posted review's URL and how many findings landed.

Fixing what it finds is `pr-review-resolver`'s, not this skill's. The findings are comment
threads now, and code changed without a reply on its thread leaves the pull request no better
off. Say that, and stop.

## What this skill never does

It never approves and never merges — approval is a separate act with its own subcommand, and
no flag here reaches it.

It never marks a handoff consumed. A caller that reads a handoff owns that bookkeeping, because
this skill runs on branches that never had one.
