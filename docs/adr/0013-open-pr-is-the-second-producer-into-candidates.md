# `open-pr` stages into `candidates/`, at the one point every path reaches

The memory store has one **Curator** and, until now, two producers: the **Explorer**, which
stages what it learned answering a question, and `/memory-seed`, which sweeps a cold codebase
once. A third existed by accident of shape — `continuation-session` extracted the durable half
of its handoff and copied it into `candidates/` — and retiring that skill vacates the slot.

`open-pr` takes it. It stages just before `gh pr create`, after the branch's work is done and
before the context that produced it is discarded.

The position is the decision. Implementing is where exploration is actually paid for: an
implementer that hit an undocumented invariant in a test harness learned something no plan-time
reading would have surfaced, and the **Coordinator** holds that in the reports it received.
`open-pr` is the single point every path through the flow reaches — a plan of one **Slice**, a
plan of several, and a **Handoff** written by hand with no plan at all — so a producer there
fires on all three and needs no second copy anywhere.

## Considered options

**`write-handoff` carries it**, as a direct port of the step it inherits. This is the obvious
reading of "the successor takes over what the retired skill did", and it is wrong for a reason
that is invisible until the flow is drawn out: `write-handoff` runs at plan approval, at a slice
boundary, and by hand — and **a plan of a single slice never reaches a slice boundary**.
`implement-handoff` opens the pull request, finds no next slice, and never invokes it again. The
step would be silently absent in the commonest case of all, which is worse than not having it,
because nothing would report the absence.

**Both, each on its own trigger.** Rejected as a smaller version of the same problem. It buys
one case — work set down and never landed, where a hand-written handoff is the last artifact —
at the price of two producers whose overlap has to be reasoned about every time either changes.
The abandoned-branch case loses a finding; the store's shape stays simple. That trade is
deliberate, and it is the one thing here somebody may reasonably want back.

**Nothing stages; name the loss.** Rejected. The **Explorer** stages during `/plan-feature`'s
delegations, which covers what *planning* learned and nothing of what *implementing* did — and
the second is the half that cost real work. A store fed only by the cheap half is one whose
notes get steadily less worth consulting.

**`wrap-session-reviewer` becomes the producer.** This was weighed once before, in the plan that
built the store, and rejected then: `continuation-session` already produced note-shaped findings
and threw them away, so redirecting it was nearly free, whereas the reviewer produces
rule-shaped output and would have been new behaviour introduced in the same change that
resurrected an agent no consumer had run. Rejected again here, on what is now the stronger
ground: the reviewer runs *inside* `open-pr`, so the skill that would dispatch it is already at
the right point in the flow and holds the implementers' reports, which the reviewer does not.

## Consequences

- `continuation-session` is retired with its staging step relocated rather than dropped. Nothing
  about the store's single-writer rule changes: `open-pr` writes `candidates/` and never
  `notes/`, and ADR 0003 stands untouched.
- A branch abandoned without a pull request stages nothing. Whatever it learned is lost unless
  somebody runs the **Explorer** at it later. This is the accepted cost of the second option
  above, and it is where to look first if the store starts feeling thin.
- The bar is unchanged and is enforced in prose in two places now — the **Explorer**'s
  definition and `open-pr`'s. A finding must be a durable fact about the codebase *and* name a
  file it came from, because a **Note** needs an **Anchor** and one that cannot point anywhere
  is rejected by lint however useful it reads.
- The **Curator**'s input grows by roughly one branch's worth of findings per pull request. It
  runs from `/memory-curate`, which nobody has automated, so a backlog is reported at session
  start rather than accumulating silently.
