---
name: suppression-requires-a-complete-verdict
kind: invariant
description: A posted review suppresses a re-review only when this installation authored it, its commit and marker head both name the head, and the marker reads verdict=complete; an unparseable marker deliberately counts as no verdict.
anchors:
  - path: source/toolkit/internal/cli/codereview_pr.go
    blob: ac97d9597d1e
  - path: source/toolkit/internal/reviewapprove/gate.go
    blob: c4b850329bf8
  - path: source/toolkit/internal/reviewrun/marker.go
    blob: 9c5196458621
  - path: source/toolkit/internal/githubapp/replies.go
    blob: fe91de20c87e
confidence: verified
---

`carriesAVerdictFor` (`source/toolkit/internal/cli/codereview_pr.go:153`) decides whether `runCodeReviewPR`
skips the panel. It does not implement the search itself — it asks
`reviewapprove.LastReview` (`source/toolkit/internal/reviewapprove/gate.go:116`) and returns
`found && marker.Complete`. `LastReview` keeps the *newest* review satisfying all of:
`ByViewer`, `CommitSHA == head`, a body `reviewrun.ParseReviewMarker` accepts, and
`marker.Head == head` (`gate.go:121-129`). Every clause carries: an older complete review must
not speak for a newer forced run that failed, or suppression would refuse the re-review while
approval refuses the head.

Head SHA alone is not enough, and the distinction is load-bearing rather than cosmetic. A run
where every reviewer failed still posts a review — that is the design, so the failure is
visible — and that review is a record that **nothing looked at the change**. Counting it as
"this head is reviewed" leaves the pull request displaying a review nobody performed, with the
marker recording the failure being the very thing that blocks the re-run that would fix it
(`codereview_pr.go:134-140`). Only `--force` gets past it — the gate is
`!flags.dryRun && !flags.force && reviewsErr == nil && carriesAVerdictFor(...)`
(`codereview_pr.go:309`), and the message that tells a caller so is at `codereview_pr.go:166`. This is
the same "could not look is not found nothing" distinction the pipeline is careful about at the
run level.

Two deliberate readings, both erring toward re-reviewing:

- A review whose body carries **no parseable marker** counts as no verdict, which `LastReview`
  delivers by finding none. Being wrong costs one panel re-run; the other direction costs a
  pull request that looks reviewed and is not (`codereview_pr.go:148-152`).
- Only `ByViewer` reviews count. Anyone who can review a pull request can type the characters
  that open a marker, so a marker is believed from one account and read as prose from all
  others — see the `ByViewer` comment on `SubmittedReview`,
  `source/toolkit/internal/githubapp/replies.go:217-220`.

`ReviewMarker.Complete` is rendered as `verdict=complete|incomplete`
(`source/toolkit/internal/reviewrun/marker.go:135-138`) and read back in the same file (`:230-233`), because a
format written in one place and parsed in another drifts silently — and here the drift shows up
as a gate nobody can pass.
