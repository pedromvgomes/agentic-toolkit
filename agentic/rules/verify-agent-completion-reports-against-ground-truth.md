---
description: "A headless agent run's self-reported completion must be checked against a before/after snapshot of what it was supposed to change, in both directions, not just trusted or spot-checked."
---

# Verify a headless run's completion report against ground truth, in both directions

A model's own account of what it did is prose from the same turn that may have stopped early,
skipped a step, or answered instead of refusing — checking only "does reality contain what the
report claims" still passes a run that changed something real and reported none of it. Confining
what a run *may* touch (its tool grant) says nothing about whether it *finished*; only comparing
a snapshot taken before the run to one taken after, against the report, catches a run that quit
partway through its own instructions.

## Applies to

Any command that hands work to a model and needs to know the job actually completed, not just
that the model said so — currently `source/toolkit/internal/curator` (`verify.go`, called from
`Run` in `curator.go`). Extend the same shape rather than inventing a new one: snapshot before,
run, snapshot after, then check claim-to-disk (everything reported happened) and disk-to-claim
(everything that happened was reported) as two separate passes.

## Example

Good: `verify` in `curator.go`/`verify.go` diffs `snapshot` taken before and after the child
runs, and fails if a note changed on disk with no matching entry in `Report.NotesTouched`, *and*
fails if a backlog run leaves a candidate in `candidates/` that the report does not resolve. A
candidate the report resolves and the run left staged is not a failure: `clearResolved` removes
it once `verify` has passed.

Bad: only checking that every name in the report resolves to something true (e.g. a "touched"
note parses and lints clean) — a run that wrote three notes and reported one would pass, because
nothing looks at what the store gained that the report never mentioned.
