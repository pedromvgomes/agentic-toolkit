---
about: a posted review suppresses a re-review only when this installation authored it and its marker reads verdict=complete; an unparseable marker deliberately counts as no verdict
saw:
  - internal/cli/codereview_pr.go
  - internal/reviewrun/marker.go
  - internal/githubapp/replies.go
---
`carriesAVerdictFor` (internal/cli/codereview_pr.go) is what decides whether
`runCodeReviewPR` skips the panel. It requires all three of: `ByViewer`, a `CommitSHA` equal to
the head, and a body whose `reviewrun.ParseReviewMarker` returns a marker with `Complete` true.

Head SHA alone is not enough, and the distinction is load-bearing rather than cosmetic. A run
where every reviewer failed still posts a review — that is the design, so the failure is
visible — and that review is a record that **nothing looked at the change**. Counting it as
"this head is reviewed" leaves the pull request displaying a review nobody performed, with the
marker recording the failure being the very thing that blocks the re-run that would fix it.
Only `--force` got past it. This is the same "could not look is not found nothing" distinction
`classify()` (internal/reviewrun/run.go) is careful about at the run level.

Two deliberate readings, both erring toward re-reviewing:

- An App-authored review whose body carries **no parseable marker** counts as no verdict. Being
  wrong costs one panel re-run; the other direction costs a pull request that looks reviewed
  and is not.
- Only `ByViewer` reviews count. Anyone who can review a pull request can type the characters
  that open a marker, so a marker is believed from one account and read as prose from all
  others (see the `ByViewer` comment on `SubmittedReview`, internal/githubapp/replies.go).

`ReviewMarker.Complete` is rendered as `verdict=complete|incomplete` and read back in the same
file (internal/reviewrun/marker.go), because a format written in one place and parsed in
another drifts silently — and here the drift shows up as a gate nobody can pass.
