---
name: completeness-reads-superseded-not-unanswered
kind: invariant
description: Review completeness is computed only through Review.Superseded()'s missing list, and Superseded needs four conditions at once, each guarding a real bug.
anchors:
  - path: source/toolkit/internal/reviewrun/report.go
    blob: 1531e689597f
  - path: source/toolkit/internal/reviewrun/run.go
    blob: 6f4a731b4d87
  - path: source/toolkit/internal/reviewpost/body.go
    blob: 69362927d619
  - path: docs/adr/0017-completeness-is-measured-against-the-panel-that-answered.md
    blob: b7541710fbcd
confidence: verified
---

`Review.Partial()` (`report.go:193`) is defined as "`Superseded()`'s missing list is non-empty".
Completeness readers go through it: `Complete` in `reviewpost/body.go:92`, and the missing list
at `body.go:198`. They must not read raw `Unanswered()`, or a block a successful fallback already
answered for counts as a gap. ADR 0017 records that this drifted twice before.

`Superseded()` (`report.go:221`) treats a run as superseded only when all hold: `r.Available`
(the fallback itself reached a verdict), `r.FallbackFrom != ""`, `run.Panel == r.FallbackFrom`
(the run was on the replaced panel, not the fallback's own attempt), and `run.Report.Blocked`.
Dropping `Available` hides a fallback that failed for an ordinary reason; dropping the `Panel`
check hides a blocked instance in the fallback panel's own quorum as if superseded.

`RunReport.Panel` exists for this: `tagPanel` (`run.go:332`, called at `:288` and `:303`) stamps
each report before the two panels' reports merge into one slice.
