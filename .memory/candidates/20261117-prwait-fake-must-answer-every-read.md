---
about: "Every read prwait makes in a poll must be answered by the fake GitHub in the CLI's pr wait tests: a route the fake does not know fails the poll, and a test run without --poll then sleeps the real five-minute interval until the test binary times out"
saw:
  - source/toolkit/internal/cli/tests/prwait_test.go
  - source/toolkit/internal/prwait/prwait.go
  - source/toolkit/internal/githubapp/checks.go
---

`prwait.read` makes every read of a poll in turn (the pull request, the workflow runs, the check
runs, the statuses, the comments, the review threads) and any error fails the whole poll. The
fake GitHub in `internal/cli/tests/prwait_test.go` (`prGitHub.handle`) answers an unknown route
with 404, so adding a read to `prwait` without teaching the fake its route turns every poll of
the CLI tests into a failed read. Tests that pass `--poll 1ms` end quickly as could-not-read;
a test that leaves `--poll` at its default waits the real five minutes between polls and the
package hits the `go test` timeout, which reads as a hang rather than a missing route.
