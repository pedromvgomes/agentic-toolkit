---
name: review-pull-request
description: |
  Review an open pull request and answer what comes back, repeatedly, until a pass leaves nothing unanswered or the cap is reached.
  The pull-request counterpart of `review-implementation`: each pass posts a panel review, then `pr-review-resolver --unattended`
  fixes or answers every open thread and pushes, and the next pass reads only what that push changed. Never resolves a thread,
  never approves, never merges. Trigger on "converge this PR", "loop the PR review", "review and fix the PR until clean",
  "answer the review on PR 123", and as the step `open-pr` runs once the pull request exists.
requires:
  - skills/panel-code-review
  - skills/pr-review-resolver
  - instructions/git
extensions:
  claude:
    argument_hint: "<pr number> [--max <n>]"
---

# Review pull request

Post a panel review on a pull request, answer every thread it opens, push, and review again.
Stop when a pass leaves nothing unanswered, when the pull request stops moving, or at the fifth
pass, whichever comes first.

This skill owns the **loop** and nothing else, the same way `review-implementation` owns the
local one. What a pass reviews, which panel runs, and what gets posted are `agtk`'s decisions
(ADR 0011). Deciding each thread and writing its reply is `pr-review-resolver`'s job. Everything
this skill knows about a pass, it reads off those two.

## Arguments

- a **pull request number**. Required: this skill never guesses which pull request to review.
- `--max <n>`: a ceiling other than 5. Values above 5 are refused. A pull request that needs a
  sixth pass is not converging, and spending more on it hides that rather than fixing it.

Follow the repo's git rules: read the GitHub user from the root `.envrc` before any `gh` call,
and export `GH_TOKEN` for that user on every call that reaches GitHub.

## What each pass reads

`agtk code-review run --pr` chooses its own range, and this skill never passes one:

- **The first review of a pull request reads the whole change.** When `open-pr` has just posted
  it, that review is pass 1. Running the command again on an unchanged head spends nothing and
  reports that the head is already reviewed.
- **A review after a push reads only what changed since the last complete review**, and its
  panel is sized from that delta. If the branch was rewritten or the base merged in, the engine
  reads the whole change again and says why (ADR 0018).

Report the scope the engine printed. Never pass `--full` on your own initiative. The engine
decides when narrowing is unsafe, and a loop that widens every pass pays the first pass's cost
five times.

## What counts as answered

A thread is **answered** when its latest comment is a reply from this loop's account, i.e. the
`gh` user this session runs as. It is **waiting** when anybody else commented last: agtk's own
review, a bot, or a person.

**Every** unresolved thread counts: agtk's, bots', and people's. A question a person asked is a
conversation about this change, and leaving it unanswered while declaring the pull request clean
misreports what the pull request carries.

Resolved threads are skipped. **This skill never resolves a thread.** Resolution is the person's
act. It is what stands between the loop's claim that something is fixed, or is not a defect,
and approval (ADR 0018).

## The loop

Each pass:

1. Run the review and keep what it printed:

   ```bash
   agtk code-review run --pr <N> --json
   ```

   Read three things off it: whether it reached a verdict (`available`, or the unchanged-head
   reply, which means an earlier pass's verdict stands), the scope it read, and its cost.
2. List the unresolved threads and mark each one **answered** or **waiting**, using the GraphQL
   query `pr-review-resolver` uses in its Phase 1. Record the waiting threads' ids and the head
   commit.
3. If any thread is waiting, invoke `pr-review-resolver <N> --unattended`. It decides each
   thread, commits and pushes its fixes, replies on every waiting thread, and verifies each
   reply. Then start the next pass.

Run passes one at a time. A review started while the previous pass's fixes are still being
pushed reads a head that is about to change.

## Stopping

Four ways out, checked in this order:

**No verdict.** The review could not look: a reviewer did not answer, an engine did not start,
or every provider was blocked. Stop and say so. A review that could not look and a review that
found nothing both leave nothing to answer, and only the second means the pull request is
clean.

**Clean.** The latest pass reached a verdict and no thread is waiting. Stop and report the pass
number. This is the outcome the loop exists for.

**Stalled.** The same threads are waiting as after the previous pass, **and** the head has not
moved. The resolver is not landing anything, and the remaining passes would spend money to
learn that again. Stop, name the waiting threads, and say that the loop stalled rather than that
it hit the cap. The two call for different things from whoever reads the report.

**Capped.** The ceiling is reached with threads still waiting. Stop and name them.

## Reporting

- One line per pass: the pass number, the panel, the scope read (delta since which commit, or
  full and why), how many RED and AMBER were posted, how many threads were answered, and the
  cost. Take the cost from the engine's record; never estimate it.
- The outcome, and the total spent across every pass.
- **Every false-positive marking the resolver posted**, with the finding and its reason. These
  are the agent's own judgements that a finding is not a defect, and whoever resolves the thread
  is agreeing with them.
- **The threads waiting for a person to resolve**, with links. Approval needs them resolved, and
  nothing in this loop resolves them.

## When the loop does not end clean

Stalled, capped and no-verdict are **failures to converge**. The pull request is open, and it is
not ready. Name what is still waiting, with its file and line, and stop. Do not tell a caller
that the pull request is done. `implement-handoff` treats anything but clean as a halt, and
leaves its handoff where it is so the work can be picked up again.

## What this skill never does

It never resolves a comment thread, never approves, and never merges. Approval is `agtk
code-review approve`, typed by a person, and it refuses until the threads this loop answered
have been resolved by one.

It never reviews the working tree. `review-implementation` does that, before anything is
published.
