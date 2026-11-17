---
about: "git-fixture builder list: handoff entries deleted, grep now returns 15 files"
saw:
  - source/toolkit/internal/cli/tests/helpers_test.go
  - source/toolkit/internal/reviewrun/run_test.go
  - source/toolkit/internal/cli/codereview_pr_test.go
targets: no-shared-git-fixture-helper
verdict: still-true
---
The claim (no shared git fixture helper; every package rolls its own) holds, but the list needs edits.
Re-ran `grep -rln 'exec.Command("git"' --include='*_test.go' source/toolkit/internal`: 15 files, not twelve:
cli/codereview_init_test.go, cli/codereview_pr_test.go, cli/codereview_scope_test.go,
cli/tests/{cloud_init,codereview_run,codereview,deterministic_surface,helpers}_test.go,
cloudinit/tests/helpers_test.go, gitremote/tests/repo_test.go, review/tests/repo_test.go,
reviewrun/run_test.go, reviewrun/tests/repo_test.go, sourcestore/tests/helpers_test.go,
usage/tests/identity_test.go.

Delete the two bullets `internal/handoff/handoff_test.go:14` (`repo`) and
`internal/cli/tests/handoff_test.go:13` (`handoffRepo`): both files are deleted. The sentence "the
handoff and code-review work added five more" should drop "handoff". New builders since the note:
cloudinit, gitremote, usage, cli/codereview_scope_test.go. Line numbers of the other bullets were not
re-verified (those files changed); re-derive with the grep.
