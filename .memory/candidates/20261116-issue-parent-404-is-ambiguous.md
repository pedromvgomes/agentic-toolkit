---
about: "GitHub's GET /issues/{n}/parent answers 404 both for an issue with no parent and for an issue that does not exist or the token cannot see, so ReadParent follows a 404 with a read of the issue itself and reports no parent only when that read succeeds; ReadParentOfVisibleIssue skips that read for a caller that already holds the issue"
saw:
  - source/toolkit/internal/githubapp/issues.go
  - source/toolkit/internal/githubapp/tests/issues_test.go
  - source/toolkit/internal/backlog/list.go
---

`(*Client).ReadParent` in `githubapp/issues.go` returns `(parent, found, err)`. On a 404 from
`/repos/{o}/{r}/issues/{n}/parent` it calls `ReadIssue(ctx, number)`: success means `found=false`
with no error, failure returns that read's error (typically `ErrNotFound`). Treating the 404 alone
as "no parent" would report a missing or invisible issue as an orphan. Both share the unexported
`readParent(ctx, number, visible)`; `ReadParentOfVisibleIssue` passes `visible=true`, so a 404 is
"no parent" with no follow-up GET. `backlog.List` already holds every item from its label scan and
reads parents through `ReadParentOfVisibleIssue` (`backlog/list.go`, `readItem`); the `Reader`
interface in `backlog/verify.go` carries only that method, not `ReadParent`.
