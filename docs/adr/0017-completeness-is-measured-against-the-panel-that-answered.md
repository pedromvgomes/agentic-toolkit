# Completeness is measured against the panel that answered, not the one it replaced

ADR 0015 gave a blocked panel a single-hop retry on its declared `fallback:`, and carried the
blocked panel's own runs into the returned `Review` rather than discarding them — deliberately,
so the record kept saying what the block cost rather than letting a substitute panel look like
the one an escalation rule actually asked for. `Review.Partial()` read every run in that merged
record, so a panel's block always left at least one unanswered run in it, however cleanly the
fallback itself went. `reviewMarker`'s `Complete` and the CLI's JSON `partial` field both read
`Partial()`, and `reviewapprove/gate.go` refuses approval on anything but `Complete`. The result:
a fallback that fully answered still posted a review permanently marked incomplete, blocking
approval for as long as the replaced provider stayed down — with the posted body giving no
reason, once it stopped presenting the blocked runs as an open gap (see the commit fixing that).

A `fallback:` is not something that happens to a review despite the manifest; it is what the
manifest asked for. Declaring one is the repo saying a panel on the other provider is an
acceptable substitute when the first is unavailable — the same shape as a panel's own quorum
saying one dissenting reviewer is an acceptable substitute for unanimous agreement. Reading the
substitute as an automatic gap treats a configured, named response to an outage as if it were an
unconfigured one, and holds every pull request hostage to a provider's uptime for exactly the
outages the feature exists to route around.

## Decision

`Review.Partial()` — and everything that reads it, without change on their part — now excludes
the runs a successful fallback answered for. `Review.Superseded()` splits `Unanswered()` into
those blocked runs (matched by which panel they actually ran under, `RunReport.Panel`, against
`FallbackFrom`) and whatever is still genuinely missing; `Partial()` is defined as "any run in
the second group." A fallback panel that reaches its own full verdict is complete. A run left
unanswered on the fallback panel's own attempt, or for an ordinary reason unrelated to a block,
is still a real gap and still marks the review partial — nothing about *that* changed.

The two now-separate questions — "did this review reach a full verdict" and "did it fall back to
get there" — are answered by two separate fields, not folded into one. `Complete`/`partial` say
the first; the JSON `fallback_from` field (and the posted body's own line naming what it
replaced) says the second. A caller that wants to know whether a clean approval happened by way
of a substitute reads both; approval itself reads only the first, because the manifest already
decided the substitute was acceptable.

## Consequences

- `RunReport` gains a `Panel` field recording which panel it was scheduled under. Without it,
  `Superseded()` could not tell a blocked run on the panel `FallbackFrom` names from a blocked
  run on the fallback panel's *own* attempt (reachable on any fallback with quorum above one) —
  and the latter is a real gap that still needs the full "Could not answer" treatment.
- A pull request can now be approved after a fallback that fully answered, without a human
  override and without the replaced provider recovering first. This is the reversal of ADR
  0015's original consequence ("The runs and cost the first, blocked panel already made are
  carried into the returned `Review`") — the runs are still carried, for the record, but no
  longer counted against completeness.
- Nothing about *when* a fallback is attempted changes: still single-hop, still only on a whole
  panel every one of whose runs came back blocked, still never chained past one retry. This ADR
  is about what the result of a successful fallback means, not about when one is tried.
