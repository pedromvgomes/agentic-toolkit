---
about: a GraphQL query that returns review bodies pages at reviewsPerPage (10), not the general pageSize (25), because a review body is the largest document on a pull request
saw:
  - internal/githubapp/replies.go
  - internal/githubapp/graphql.go
  - internal/githubapp/client.go
---
`internal/githubapp` has two page-size constants and they are not interchangeable.
`pageSize = 25` (internal/githubapp/graphql.go) is for reads that carry no body — a commit oid,
a thread's first comment. `reviewsPerPage = 10` (internal/githubapp/replies.go) exists because
a review body "states everything the review found that no comment carries, so it is the largest
single document on a pull request".

The bound that makes this matter is `maxBody = 1 << 20` (internal/githubapp/client.go), which
`io.LimitReader` applies to every response. GitHub accepts review bodies far larger than a
kilobyte and they come from anyone who can review the pull request, so a page of 25 full bodies
can overrun the limit and arrive as a JSON parse failure rather than as data. In the review
path that surfaces as `priorThreads` reporting the threads unreadable, and the run reposting
findings somebody has already answered.

`ReadSubmittedReviews` (internal/githubapp/replies.go) is the **only** reader of review bodies,
serving both approval and the pre-panel "has this head been reviewed" check. A second, cheaper
query returning the commit alone existed once and was removed: once the suppression check
needed the marker, the cheap query could not answer the question it was there to answer, and
keeping it meant two requests to the same connection for the same reviews — one of them sized
wrongly for what it now had to carry.
