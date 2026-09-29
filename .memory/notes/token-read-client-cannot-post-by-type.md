---
name: token-read-client-cannot-post-by-type
kind: invariant
description: githubapp.ReadClient cannot post because it wraps rather than embeds *Client, but only a reflection test stops it gaining a non-Read method.
anchors:
  - path: source/toolkit/internal/githubapp/client.go
    blob: f2a998cd4b15
  - path: source/toolkit/internal/cli/tests/credential_surface_test.go
    blob: 553d921a7471
  - path: docs/adr/0019-a-token-reads-a-pull-request-and-never-posts.md
    blob: 808576ffd485
confidence: verified
---

`ReadClient` (`source/toolkit/internal/githubapp/client.go:105`) holds an unexported `c *Client`
and does not embed it, so nothing `*Client` has is promoted. Its only methods are
`ReadPullRequest`, `ReadReviewThreads` and `ReadSubmittedReviews` (`:121`, `:132`, `:150`).
`reviewapprove.GitHub` needs `CreateReview`, and posting sites call `CreateReview` /
`CreateFileComment` on `*githubapp.Client`, so wiring a `ReadClient` into either is a compile
error.

That holds only while no other method is added; the compiler does not forbid one.
`TestATokenClientReadsAndNothingElse` (`credential_surface_test.go:351`) reflects over the
exported methods, fails on any not prefixed `Read`, and asserts the type does not implement
`reviewapprove.GitHub`. Weakening it turns a compile-time impossibility into a convention.
