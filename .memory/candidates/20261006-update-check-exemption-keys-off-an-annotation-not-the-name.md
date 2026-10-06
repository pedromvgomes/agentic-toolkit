---
about: "A command is exempted from the background update check through an annotation, because cmd.Name() is not unique: `agtk cloud init` and the top-level `agtk init` are both named `init`"
saw:
  - source/toolkit/internal/cli/root.go
  - source/toolkit/internal/cli/cloud.go
  - source/toolkit/internal/cli/tests/cloud_init_test.go
---

The persistent pre-run in `root.go` starts the background update check unless the command is exempt.
`update` is exempted by `cmd.Name() != "update"`, which is a match on the leaf name only. A session-start
hook runs `agtk cloud init` in every session, so it must not make a network call, but its leaf name
is `init`, the same as the top-level `agtk init`, and a name match would exempt both.

`cloud init` therefore sets `Annotations[annotationSkipUpdateCheck]`, and the pre-run checks the
annotation. `cloud_init_test.go` asserts both halves: `agtk init` still starts the check and
`agtk cloud init` does not. Observing that needs `Env.StartUpdateCheck`, a test seam, because the
default gates already refuse a non-terminal stdout and a test cannot otherwise tell whether a check
was started.

The `update` exemption is still a name match, so a future subcommand named `update` anywhere in
the tree would skip the check too.
