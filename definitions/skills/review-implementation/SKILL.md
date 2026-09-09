---
name: review-implementation
description: |
  Review a branch and fix what comes back, repeatedly, until a pass finds no defect or the cap is reached. Wraps `panel-code-review`
  on the local target with a five-pass ceiling and an early exit, so a change is reviewed-and-fixed as one act rather than as a
  sequence somebody has to keep restarting. Trigger on "review and fix my branch", "review until clean", "loop the review",
  "fix what the review finds", and as the step `implement-handoff` runs after its last task lands.
requires:
  - skills/panel-code-review
---

# Review implementation

Run `panel-code-review` over the local target, apply what it finds, and run it again. Stop when
a pass finds no defect, when the change stops moving, or at the fifth pass — whichever comes
first.

This skill owns the **loop** and nothing else. It has no prompt, no roster, no severity ladder
and no engine invocation of its own, because each of those already exists in the review
manifest or in `agtk`, and a second copy in prose is a second answer that drifts from the
tested one (ADR 0011). Everything it knows about a pass, it learns from what
`panel-code-review` reports.

## Arguments

- `--max <n>` — ceiling other than 5. Values above 5 are refused: a change needing a sixth pass
  is not converging, and spending more on it hides that rather than fixing it.
- `--dry-run` — run one pass, report, fix nothing. For seeing where a branch stands.

## What counts as a defect

RED and AMBER. `CONTEXT.md` defines both as defects differing in the strength of the claim
rather than in what they oblige, so the loop treats them alike. GREEN is a remark and never
keeps the loop running — a branch whose only findings are GREEN is done.

## What each pass looks at

**Pass 1 reviews the whole change** — everything on the branch, against the base it will merge
into. This is the pass that has to see every line, because nothing else in the flow gives the
branch an independent reading: the coordinator read each task's diff as it landed, and that is
the same model that accepted it.

**Every later pass reviews only what changed since the pass before it.** Record the head commit
after each pass's fixes are applied, and make it the next pass's base:

```bash
git rev-parse HEAD          # after pass N's fixes — this is pass N+1's base
```

The rest of the branch was read one pass ago and found clean. Reviewing it again costs the same
as the first pass and asks a question that has been answered, which is how a loop that converges
still ends up costing five times its first pass.

## The loop

Each pass:

1. Invoke `panel-code-review` with the **local target** and `--auto-fix`, over the range above:
   the whole branch on pass 1, and the previous pass's head as the base after that. Say "my
   branch" so the target resolves without a question; an open PR must not turn this into a PR
   review, because a PR review posts, and this loop happens before anything is published.
2. Read three things off what it reports: whether the run reached a verdict, the surviving
   RED and AMBER count, and their fingerprints.
3. Decide whether to run again.

Run the passes one at a time. Two reviews of the same branch at once read different working
trees, because the first one's fixes land while the second is reading.

## Stopping

Four ways out, checked in this order:

**No verdict.** The run could not look — a reviewer that failed to answer, an engine that could
not start. Stop immediately and say so. A run that could not look and a run that found nothing
both report zero defects, and only one of them means the branch is clean; treating the first as
the second is how a review silently stops being one.

**Clean.** No RED and no AMBER survive. Stop and report the pass number. This is the outcome
the loop is for.

**Stalled.** A pass's surviving fingerprints are the same set as the previous pass's. The
fixes are not landing, and the remaining passes will spend money to learn that again. Stop,
report the surviving findings, and say the loop stalled rather than that it was capped — the
two call for different things from whoever reads it.

**Capped.** The ceiling is reached with defects still standing. Stop and report them.

## Reporting

One line per pass — the pass number, the panel that ran, how many RED and AMBER survived, and
what it cost — then the outcome. The per-pass cost comes from the engine's own record; do not
estimate it.

End with the total spent across every pass. A loop is the one place where the cost of a review
is not obvious from a single number, and somebody deciding whether to raise `--max` needs it.

## When the loop does not end clean

Stalled and capped are **failures to converge**, and they oblige the same thing: the branch is
not ready. Say which findings survive, with their file and line, and stop.

Do not open a pull request, and do not tell a caller it may. `implement-handoff` treats a
non-clean outcome as a halt — the handoff stays unconsumed so the work can be resumed — and a
caller that reads "capped" as "good enough" ships the defects the loop already found and
reported.

## What this skill never does

It never reviews a pull request. `panel-code-review` does that, and posts when it does; this
loop exists on the other side of that line, and a flag that crossed it would make every pass
publish.

It never approves anything, and never resolves a **Comment thread**.
