---
about: "The agtk backlog CLI tests fake GitHub by swapping http.DefaultTransport, because the black-box internal/cli/tests package cannot reach clientSeam's unexported doer and Env has no transport field; those tests use t.Setenv, so they never run in parallel"
saw:
  - source/toolkit/internal/cli/tests/backlog_test.go
  - source/toolkit/internal/cli/backlog.go
  - source/toolkit/internal/cli/codereview_pr.go
---

`backlog.go` builds its reader with `githubapp.NewReadClient(token, repo, seam.options()...)`,
whose `http.Client` has no transport of its own, so it uses `http.DefaultTransport`. The tests in
`cli/tests/backlog_test.go` replace that global with a fake RoundTripper and restore it in
`t.Cleanup`. This is safe only because they also call `t.Setenv`, which forbids `t.Parallel`, and
sequential top-level tests finish before paused parallel ones resume. A new parallel test in that
package that makes real HTTP calls through the default client would not be affected while these
run, but a new backlog test that calls `t.Parallel` would race on the global.
