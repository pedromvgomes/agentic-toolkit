---
about: nothing in the review-thread pipeline carries the GraphQL node id a resolveReviewThread mutation needs
saw:
  - source/toolkit/internal/githubapp/threads.go
  - source/toolkit/internal/reviewpost/threads.go
  - source/toolkit/internal/reviewrun/thread.go
---

`reviewThreadsQuery` (`source/toolkit/internal/githubapp/threads.go:44-58`) selects `path`,
`isResolved`, `isOutdated` and the first comment's `body`/`viewerDidAuthor` — never `id`. The
decode struct in `ReadReviewThreads` (`threads.go:80-93`) mirrors exactly that set, and
`ReviewThread` (`threads.go:15-34`) has no id field to decode into.

Neither of the two translation layers above it carries one either:
`reviewpost.ReadThreads` (`source/toolkit/internal/reviewpost/threads.go:15-34`) builds a
`reviewrun.Thread` from `Path`, `Resolved`, `Outdated`, `Body`, and — only when `ByViewer` and
the body parses — `Version`/`Fingerprint`. `reviewrun.Thread` (`source/toolkit/internal/reviewrun/thread.go:14-29`)
has the same fields and nothing else.

GitHub's `resolveReviewThread` mutation takes a review thread's node `id`, not its path or
comment id. Wiring "resolve the thread whose finding is no longer reported" through this
pipeline means adding `id` to the GraphQL selection, the decode struct, `ReviewThread`,
`reviewpost.ReadThreads`'s translation, and `reviewrun.Thread` — four call sites, none of
which currently have anywhere to put it — plus a new mutation call in `internal/githubapp`
(none exists there today; `grep -rn resolveReviewThread source/toolkit` is empty).

No extra GitHub App permission is implied by the mutation itself — resolving a thread is a
pull-request-level write the same `pull_requests: write` grant already covers (unlike ADR
0009's `contents: write`, which exists solely so an *approval* counts) — but nothing in
`internal/githubapp` currently asserts what permission scopes the App holds; there is no
permission/scope check anywhere in that package (`grep -in permission source/toolkit/internal/githubapp/*.go`
finds nothing but incidental words).
