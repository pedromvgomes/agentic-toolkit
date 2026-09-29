---
about: A provider missing from PATH is reported Missing and triggers the panel's fallback, but never sets Blocked; Blocked has consumers beyond the fallback (posting, Superseded, the approval Complete marker), which is why the two flags are separate
saw:
  - source/toolkit/internal/reviewrun/run.go
  - source/toolkit/internal/reviewrun/invoke.go
  - source/toolkit/internal/reviewrun/schedule.go
  - source/toolkit/internal/reviewrun/report.go
  - source/toolkit/internal/cli/codereview_pr.go
  - source/toolkit/internal/reviewpost/body.go
  - docs/adr/0015-a-block-tries-the-other-provider-and-never-posts.md
  - docs/adr/0021-a-missing-provider-tries-the-fallback-and-still-posts.md
targets: missing-provider-is-unavailable-not-blocked
verdict: now-false
---

The target note's claim no longer holds: a missing provider does trigger the fallback. It is still
never `Blocked`, and a bad schema, a sandbox refusal or a timeout is still neither (ADR 0015).

A run is Missing (`Report.Missing`, `report.go:32`) only when the error arose before anything
started: `build` returns `agentic.New`'s `ErrProviderUnavailable` wrapped in `errMissingProvider`
(`invoke.go:57`), and every error from `drv.Ready()` (`invoke.go:112`) is wrapped the same way.
The driver wraps timeouts, cancellations and stream errors in that same sentinel, so
`errors.Is(err, agentic.ErrProviderUnavailable)` alone would misclassify them; errors from
`drv.Run` are never wrapped. `classify` (`invoke.go:173`) and `neverStarted` (`schedule.go:168`,
the scheduler's `Limit` path) turn the marker into `Missing(...)`; anything else stays `Unavailable`.

`unansweredCause` (`run.go:358`) answers two questions over the unanswered runs: `reroutable`
(all Blocked or Missing) gates the single-hop fallback (`run.go:294`), and `blocked` (all Blocked)
alone sets `out.Blocked` (`run.go:301`) and `alt.Blocked` (`run.go:319`, read from the fallback
panel's own runs). A mixed Blocked plus ordinary failure panel is neither, so it posts visibly.

`Blocked` keeps consumers a Missing run must not reach:
- `deliverReview` (`cli/codereview_pr.go:763`): `result.Blocked` means nothing is posted. A missing
  binary posts its no-verdict like any outage.
- `Review.Superseded` (`report.go:235`) counts a Blocked or Missing run on the fallback's origin
  panel as superseded once the twin answers; `Partial()` feeds `Complete` in the review marker
  (`reviewpost/body.go`), which `reviewapprove` reads.
- Rendering names the cause through `Review.FallbackCause` (`report.go:254`) and `fallbackCause`
  (`reviewpost/body.go:276`), which walk the first panel's carried reports themselves because
  `Superseded` is empty when the twin did not answer.

The Missing report's `Reason` is the driver's own message: the sentinel's text is empty and it is
joined with no separator, so the binary name and install hint show through.
