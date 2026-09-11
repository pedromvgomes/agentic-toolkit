---
name: write-handoff
description: |
  Write the handoff a fresh session reads to continue this work — the goal, the state, the next steps, and optionally a plan's
  tasks in landing order with the pull request each slice is gated behind. Owns the handoff format; written by /plan-feature after
  approval, by implement-handoff for a following slice, and by hand when a session runs out of context mid-work. Trigger on
  "write a handoff", "hand this off", "I'm out of context", "save my place", "continue this in a fresh session".
---

# Write handoff

Write the document a session with no memory of this one reads to continue the work.

There is one handoff format and this skill owns it. A handoff carries a goal, the current state
and the next steps; a plan's task list, its slices and its predecessor are **optional**, and a
handoff written by hand mid-work carries none of them and is still a handoff. Requiring them
would make the format serve planning only, and the case it would stop serving — running out of
context halfway through something — is the one that needs it most.

Read `references/handoff-template.md` before writing. It is the shape; this file is the rules.

## 1 — Establish the goal

From the argument if one was given. From the conversation if it is unambiguous. Otherwise ask —
the goal is the filter that decides what goes in and what is deliberately left out, and a
handoff written without one carries everything, which is the same as carrying nothing.

## 2 — Leave the worktree recoverable

A handoff must never point at work that only exists in a context about to be discarded.

- No uncommitted or unstaged changes. Commit them, or say in the handoff exactly why they are
  being left and what they are.
- Any commits on the branch are pushed. A branch that exists only locally is one machine away
  from being the only copy.

If you cannot reach that state — mid-refactor, nothing committable — **stop and say what is
blocking it**. Do not write a handoff that points at a dirty worktree.

## 3 — Decide the predecessor

A handoff that names a predecessor must also name the branch to create and the base to create it
from. The next session makes that branch once the pull request merges, and the handoff is the
only thing it has to read either name off.

The next session continues in **this** worktree. That is not a choice the handoff makes, and
there is no "start a new worktree" option: the worktree is where the work is.

What the handoff does decide is whether the next session may start immediately:

- **A pull request is open for this branch** → record it as the **predecessor**. The next
  session waits for it to merge, then branches off the updated base in this same worktree.
- **No pull request is open** → no predecessor. The next session resumes on this branch, now.
  This is the ordinary shape when a handoff exists only to buy a clean context.

When the plan spans several pull requests, each slice's handoff names the slice before it as
its predecessor. The first slice has none.

## 4 — Keep `handoff/` out of the repository

The folder is a fact about how somebody works, not about the project, so it is excluded per
checkout rather than written into the consumer's `.gitignore`:

```bash
exclude="$(git rev-parse --git-common-dir)/info/exclude"
grep -qxF 'handoff/' "$exclude" 2>/dev/null || echo 'handoff/' >> "$exclude"
```

`--git-common-dir` rather than `--git-dir`: under a `gt` bare repo it resolves to the shared
`.bare/`, so one write covers every worktree, and a per-worktree `.git` would need the same
line again in each.

Do this before writing the file, and never edit the consumer's `.gitignore`.

## 5 — Write it

`handoff/<YYYYMMDD-HHMM>-<short-slug>.md` at the worktree root, following the template.

Carry only what the goal needs. Do not duplicate what a plan, an ADR, an issue or a commit
already records — reference it by path or URL. The one thing worth repeating is reasoning that
exists nowhere but this conversation.

Rate each task `routine` or `intricate`. The rating picks the model that implements it, so rate
by how much judgment the task needs, not by how many lines it touches: a wide mechanical rename
is `routine`, and a small change to something with an invariant nobody wrote down is
`intricate`.

Name the files each task may touch. That list is the implementer's boundary, so a task whose
files cannot be named yet is a task that has not been thought through — split it or say so.

## 6 — Say what happens next

You cannot clear the context or start the next session yourself. That is the user's step.

Output the handoff's path, then tell the user to run `/clear`. Nothing else in the flow changes
the model, and the session that comes back is on the settings default — which is what the
implementing half runs on.

Do not paste a continuation prompt. The session-start hook finds the handoff and says what to
invoke; a prompt to paste as well would be a second way in, and the two would drift.
