# Glob anchors mark quantified claims, not merely multi-file ones

A note is anchored per file by default. It gets a glob anchor — `internal/cli/*.go` — when its
claim **quantifies over a file set**: *every*, *only*, *no* X in this directory. A claim that
merely happens to touch several files gets one anchor per file, listed.

The rule reads backwards at first glance, because the multi-file case is the one that *looks*
like it wants a glob. What decides it is not how many files the claim was derived from but
what would falsify it. A quantified claim is falsified by a file that does not exist yet, and
a per-file anchor can never notice one appearing. `only-lock-resolves-refs` is the instance
that produced this rule: it anchored five named files in `internal/cli/`, while its own body
named the failure mode as "picking the wrong provider in a *new* command". A sixth file
constructing a `LiveProvider` breaks the invariant and `agtk memory audit` reports nothing —
the note stays green while the thing it protects is gone.

An earlier phrasing said to glob "when the claim is about the absence of something". That is
the same rule seen from one side, and it is too narrow to fire: "every command except `lock`
uses `FrozenProvider`" is a claim about absence, but nobody reads it as one. Quantification is
the property a curator can actually check in the sentence it just wrote.

## Considered options
- **Glob by default, narrow by exception.** Catches every new-file falsification. Rejected:
  most notes are about specific code, and directory anchors go stale on every unrelated edit
  in the directory — churn that trains a reader to ignore the staleness signal, which is the
  one thing the whole anchoring scheme exists to preserve.
- **Let the note's `kind` decide.** The open question this closes had assumed the answer
  "probably differs by kind". It does not. Invariants are simply the kind most often
  quantified; a quantified `gotcha` wants a glob and an unquantified `invariant` does not.

## Consequences
- A glob anchor stores its expanded matches rather than one hash over the set, so `audit` can
  name the file that appeared. That is what makes this rule useful rather than merely correct,
  and it is why ADR 0001's per-match expansion is a prerequisite.
- Re-anchoring an existing note to a glob is a curator action like any other: it goes through
  a candidate with a verdict, never a hand edit. Applying this rule to the store is therefore
  a curator sweep, not a migration script.
- `**` stays rejected. A note anchored one directory level is honest about what it watches; a
  silently truncated recursive glob would look fully anchored while missing anything added
  deeper down.
