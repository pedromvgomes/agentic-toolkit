---
about: Review completeness (Partial/Complete, the posted marker, the JSON partial field, and Record()) is computed from Review.Superseded()'s missing list, not from raw Unanswered() — a block a successful fallback already answered for does not count as a gap
saw:
  - source/toolkit/internal/reviewrun/report.go
  - source/toolkit/internal/reviewrun/run.go
  - source/toolkit/internal/reviewpost/body.go
  - docs/adr/0017-completeness-is-measured-against-the-panel-that-answered.md
---

`Review.Partial()` is defined in terms of `Review.Superseded()` (`report.go`), which splits
`Unanswered()` into two groups: blocked runs a successful fallback already covers, and whatever
is still genuinely missing. `Partial()` counts only the second group. Everything that used to
read `Partial()` or `Unanswered()` directly for a completeness verdict — `reviewMarker`'s
`Complete` in `body.go`, the CLI's JSON `partial` field, and `Review.Record()`'s "N could not
answer" line — now goes through this one function, specifically so they cannot drift apart
again (that drift, twice, is what `docs/adr/0017-completeness-is-measured-against-the-panel-that-answered.md`
exists to fix).

`Superseded()` only reclassifies a blocked run as superseded when **all** of: `r.Available` is
true (the fallback itself reached a verdict — not just was attempted), `r.FallbackFrom != ""`,
`run.Panel == r.FallbackFrom` (the run was on the panel that got replaced, not the fallback
panel's own attempt), and `run.Report.Blocked` is true. Each condition guards a real bug found
by review: dropping `r.Available` let a fallback that itself failed for an ordinary reason still
report the original block as "answered by the fallback"; dropping the `run.Panel` check let a
blocked instance in the fallback panel's *own* quorum (reachable on any fallback with quorum
above one, e.g. `focused`/`deep`) get hidden as superseded instead of reported as the real gap
it is.

`RunReport` gained a `Panel` field (`run.go`'s `tagPanel`, called once per `runPanel` attempt
before `Run` merges the replaced panel's reports with the fallback's) specifically so
`Superseded()` could tell the two panels' runs apart after they land in one `Reports` slice —
without it there is no way to distinguish "blocked on the panel we retried away from" from
"blocked on the panel we retried to."
