---
name: review-bodies-have-their-own-page-size
kind: gotcha
description: A GraphQL query returning review bodies pages at reviewsPerPage (10), not the general pageSize (25), because a review body is the largest document on a pull request.
anchors:
  - path: internal/githubapp/replies.go
    blob: fe91de20c87e
  - path: internal/githubapp/graphql.go
    blob: 5ab99057e716
  - path: internal/githubapp/client.go
    blob: 07aa4a23bac6
confidence: verified
---

`internal/githubapp` has two page-size constants and they are not interchangeable.
`pageSize = 25` (`internal/githubapp/graphql.go:99`) is for reads that carry no body — a commit
oid, a thread's first comment. `reviewsPerPage = 10` (`internal/githubapp/replies.go:206`)
exists because a review body states everything the review found that no comment carries, so it
is the largest single document on a pull request.

The bound that makes this matter is `maxBody = 1 << 20` (`internal/githubapp/client.go:31`),
applied by `io.LimitReader` to every response (`client.go:137`). GitHub accepts review bodies
far larger than a kilobyte and they come from anyone who can review the pull request, so a page
of 25 full bodies can overrun the limit and arrive as a JSON parse failure rather than as data.
In the review path that surfaces as `priorThreads` reporting the threads unreadable, and the
run reposting findings somebody has already answered.

`ReadSubmittedReviews` (`internal/githubapp/replies.go:231`) is the **only** reader of review
bodies, serving both approval and the pre-panel "has this head been reviewed" check
(see [[suppression-requires-a-complete-verdict]]). A second, cheaper query returning the commit
alone existed once and was removed: once the suppression check needed the marker, the cheap
query could not answer the question it was there to answer, and keeping it meant two requests
to the same connection for the same reviews — one of them sized wrongly for what it now had to
carry.
