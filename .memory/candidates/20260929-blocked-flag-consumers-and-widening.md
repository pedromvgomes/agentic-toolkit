---
about: Blocked is not only the fallback trigger; it also suppresses posting, drives Superseded and the approval Complete marker, so counting a missing provider as Blocked changes far more than fallback
saw:
  - source/toolkit/internal/reviewrun/run.go
  - source/toolkit/internal/reviewrun/invoke.go
  - source/toolkit/internal/reviewrun/schedule.go
  - source/toolkit/internal/reviewrun/report.go
  - source/toolkit/internal/cli/codereview_pr.go
  - source/toolkit/internal/reviewpost/body.go
  - docs/adr/0015-a-block-tries-the-other-provider-and-never-posts.md
targets: missing-provider-is-unavailable-not-blocked
verdict: still-true
---

Re-read `classify` (`invoke.go:135`; `case err != nil` at `:137`, `res.Blocked` at `:139`) and the
scheduler's never-started path (`schedule.go:151`): both still yield `Unavailable`. Note pointers hold.

Per-run `Report.Blocked` (`report.go:26`) and Review-level `Review.Blocked` (`report.go:136`) have these consumers:
- `onlyBlocked` (`run.go:344`) gates fallback (`run.go:289`) and sets `out.Blocked` when no fallback is declared (`run.go:295`) or the fallback also blocked (`run.go:324`).
- `deliverReview` (`cli/codereview_pr.go:763`): `result.Blocked` means nothing is posted (same path as --no-post) and `unavailableError` still returns an error. A missing binary counted as Blocked would go silent instead of posting visibly, which is the visibility ADR 0015 (lines 11-19, 31-34) keeps for ordinary failures.
- `Review.Superseded` (`report.go:223`) treats per-run Blocked runs as superseded once a fallback answers; `Partial()` feeds `Complete` in the review marker (`reviewpost/body.go:92`), which reviewapprove reads. A widened per-run flag would change what counts as a gap there.
- Rendering: `render.go:18` and `reviewpost/body.go:29-31` hardcode "(provider blocked)" / "every run ... was blocked" wording. `cli/jsonout.go:496,976` emits `blocked`; per-run rows (`jsonout.go:511-515`) omit per-run Blocked.

Fallback is single-hop (`run.go:299` calls `runPanel`, never `Run`); mixed Blocked+Unavailable panel: `onlyBlocked` returns false (`run.go:350-352`), no fallback, posts visibly. Fallback also requires `!out.Available` (`run.go:289`).

Unverified: where the driver (agentic-driver v0.10.0 in go.mod; source not in the module cache here) raises ErrProviderUnavailable. `Invoke` calls `drv.Ready()` (`invoke.go:78`) before `Run`, and the gate lookup (`schedule.go:53` via `Limit`, `invoke.go:65`) builds a driver without `Ready`, so the never-started path can only see it if `build`/`MaxConcurrentRuns` returns it. ADR 0015 line 79 says only claudecode reports a block "as of v0.7.0"; go.mod is now v0.10.0, so that sentence may be stale.
