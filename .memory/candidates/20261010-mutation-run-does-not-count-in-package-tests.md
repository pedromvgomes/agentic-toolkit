---
about: "A mutant that only a test in the package's own `package x` test files kills still survives in CI's lydite mutation run, so a value returned beside an error that no external test can observe needs an exclude_from_mutation marker"
saw:
  - source/toolkit/internal/cloudinit/git.go
  - source/toolkit/internal/cloudinit/git_test.go
  - .lydite/components.yml
---

`internal/cloudinit` has in-package tests (`package cloudinit` in `git_test.go` and
`signing_test.go`) beside its external ones (`cloudinit/tests/`). The mutants at `git.go:104` and
`git.go:106` (`return "", fmt.Errorf(...)` with `""` replaced by `"lydite"`) are killed by
`git_test.go` when applied by hand: `go test ./internal/cloudinit/...` fails in the package itself
while `cloudinit/tests` still passes. In CI the same two mutants were reported as survived, so the
lydite mutation run does not credit a kill that only an in-package test makes. Inference from that
observation; lydite's source was not read.

The practical rule: assert in the external test package whatever must kill a mutant. A value
returned beside a non-nil error that every caller discards (`git()`'s output at `git.go:104/106`,
`hasLocal`'s bool at `git.go:85`, `workTree`'s path at `git.go:59/69`) cannot be observed through
the public API, so those lines carry `// [lydite:exclude_from_mutation][reason]`. The in-package
tests still pin the values; they just do not count toward the CI verdict.

Established by applying each of `git.go:104` and `:106` by hand with the in-package tests present
(the package failed) and comparing with the CI mutation log of the same commit, where both were
listed as survived until the markers were added. Lydite lists each marker as a referral a human
clears.
