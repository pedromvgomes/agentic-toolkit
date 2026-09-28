---
description: Anything a non-App GitHub credential reads must report ByViewer as false.
---

# Reads made under a non-App credential must always clear ByViewer

`ByViewer` means the App's installation wrote the review or comment; `reviewapprove` and
`reviewrun` trust it to decide which head is already reviewed and which thread names a finding.
Left as GitHub reports it, a token belonging to the change's own author would make the author's
comments read as the App's markers — silencing a finding on the author's own say-so (see ADR
0019).

## Applies to

`internal/githubapp.ReadClient.ReadReviewThreads` and `ReadSubmittedReviews`, and any future
read path that can run under a credential other than the App's installation token.

## Example

Good: `ReadClient.ReadReviewThreads`/`ReadSubmittedReviews` force `ByViewer = false` on every
item before returning it, so a token-backed read never suppresses a finding or short-circuits a
re-review.

Bad: returning GitHub's own `ByViewer` value unmodified when the caller is not authenticated as
the App.
