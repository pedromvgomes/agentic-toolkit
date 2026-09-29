---
name: missing-provider-is-unavailable-not-blocked
kind: gotcha
description: A review provider missing from PATH triggers the panel's single-hop fallback (reported Missing) but never sets Blocked; Blocked alone suppresses posting, so the two flags stay separate.
anchors:
  - path: source/toolkit/internal/reviewrun/invoke.go
    blob: dc107d021fe6
  - path: source/toolkit/internal/reviewrun/run.go
    blob: ea6434ee6e7c
  - path: source/toolkit/internal/reviewrun/report.go
    blob: f8e5731b1ba5
  - path: source/toolkit/internal/cli/codereview_pr.go
    blob: 51aae07b3ca6
  - path: docs/adr/0015-a-block-tries-the-other-provider-and-never-posts.md
    blob: a361b3b6bdaa
  - path: docs/adr/0021-a-missing-provider-tries-the-fallback-and-still-posts.md
    blob: ed202cfdf51e
confidence: verified
---

`codex: executable file not found in $PATH` is expected on a machine without the codex CLI. A
missing provider does trigger the fallback (ADR 0021) but is never `Blocked`; a bad schema, sandbox
refusal or timeout is neither (ADR 0015).

A run is Missing (`Report.Missing`, `report.go:32`) only when the error arose before anything
started: `build` returns `agentic.New`'s `ErrProviderUnavailable` wrapped in `errMissingProvider`
(`invoke.go:57`), and every error from `drv.Ready()` (`invoke.go:112`) is wrapped the same way. The
driver wraps timeouts, cancellations and stream errors in that same sentinel, so
`errors.Is(err, agentic.ErrProviderUnavailable)` alone would misclassify them; errors from
`drv.Run` are never wrapped. `classify` (`invoke.go:173`) and `neverStarted` (`schedule.go:168`)
turn the marker into `Missing(...)`; anything else stays `Unavailable`.

`unansweredCause` (`run.go:358`) answers two questions over the unanswered runs: `reroutable` (all
Blocked or Missing) gates the single-hop fallback (`run.go:294`), and `blocked` (all Blocked) alone
sets `out.Blocked` (`run.go:301`) and `alt.Blocked` (`run.go:319`). A mixed Blocked plus ordinary
failure panel is neither, so it posts visibly.

`Blocked` has consumers a Missing run must not reach:
- `deliverReview` (`cli/codereview_pr.go:763`): `result.Blocked` means nothing is posted; a missing
  binary posts its no-verdict like any outage.
- `Review.Superseded` (`report.go:235`) counts a Blocked or Missing run on the fallback's origin
  panel as superseded once the twin answers; `Partial()` feeds `Complete` in the review marker
  (`reviewpost/body.go`), which `reviewapprove` reads.
- Rendering names the cause through `Review.FallbackCause` (`report.go:254`) and `fallbackCause`
  (`reviewpost/body.go:276`).

The Missing `Reason` is the driver's own message (the sentinel's text is empty), so the binary name
and install hint show through. Provider lookup is lazy in the driver per run;
`review.CheckCapabilities` never checks that a binary exists. The cross-model property in
`review/default.yaml`'s header is roster intent, not an enforced invariant.
