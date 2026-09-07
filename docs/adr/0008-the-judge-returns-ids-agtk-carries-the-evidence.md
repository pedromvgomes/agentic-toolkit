# The judge returns ids; agtk carries the evidence forward

Every candidate finding that reaches the judge is given an id by `agtk` first — a short label
that means nothing outside the run. The judge's answer is a list of those ids with a final
severity and its own prose, and nothing else. `agtk` re-attaches `file`, `line` and `evidence`
from the candidate it issued the id for.

The judge is a merging and re-severitying pass, so the fields it would otherwise re-emit are
fields it has no new information about. A judge that restates a path is a judge that can
mistype one, and a judge that restates a quote is a judge that can paraphrase it. Both failures
are silent: a re-typed path still looks like a path, and a paraphrased quote still looks like
evidence.

The quote is the field that cannot survive being re-emitted. A **Fingerprint** is the path, the
category and the normalised quote, hashed, and it is what decides whether a finding posted on
an earlier run is the same finding on this one. If the judge rewrites the quote — tidying
whitespace, trimming a line, correcting what it reads as a typo — the hash moves, the earlier
thread stops matching, and a finding somebody already resolved is posted again as new. Carrying
the quote forward byte-for-byte from the reviewer that produced it makes identity depend on one
run rather than two.

Line numbers argue the same way for a different reason. An inline comment must land on a line
that exists in the PR head's diff or GitHub rejects the entire review with a 422 — one bad
number costs every comment in the batch, including the ones that were right.

An id the judge returns that `agtk` did not issue is discarded, and the run reports that it was.
The alternative readings are both worse: trusting it means posting a finding with no evidence
behind it, and failing the run means one hallucinated label throws away a panel that has already
been paid for.

## Considered options

**The judge returns whole findings.** The obvious shape, and what the `deep-code-review` skill's
consolidation step does. Rejected on the fingerprint: identity is the quoted code, so any pass
that can rewrite the quote can break identity, and this one has no reason to touch it.

**The judge returns ids and nothing else — severity included.** Tempting, since it makes the
judge purely a filter. Rejected because re-severitying is most of what the judge is for. A
finding two reviewers reached independently and a validator upheld should outrank one that
arrived alone, and that judgement has to come out somewhere.

**Validate the judge's echoed fields against the candidates instead.** Keeps the obvious shape
and catches drift by comparing. Rejected as strictly more machinery for a strictly worse
outcome: it has to decide what to do about every mismatch, and the only defensible answer to
"the judge changed the quote" is to use the original — which is this decision, reached after
paying for the echo.

## Consequences

- The judge's schema is small: an id, a severity, a body, plus a free "what's good" list that is
  attributed to nothing and therefore needs no identity.
- A judge run cannot introduce a finding. It narrows the set it was handed and re-ranks what
  survives, which is the same separation ADR 0006 makes between deciding and transmitting, one
  layer further in.
- Ids are per-run and are never persisted. Identity across runs is the fingerprint, which is
  computed from the fields `agtk` carried rather than from anything the judge returned.
- A reviewer that quotes badly still produces an unstable fingerprint. This decision stops the
  judge from adding a second source of instability; it does not remove the first.
