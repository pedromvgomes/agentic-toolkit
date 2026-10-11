---
name: update-check-exemption-keys-off-an-annotation
kind: gotcha
description: "A command is exempted from the background update check through an annotation, because cmd.Name() is not unique: `agtk cloud init` and top-level `agtk init` are both named init."
anchors:
  - path: source/toolkit/internal/cli/root.go
    blob: 90243a953390
  - path: source/toolkit/internal/cli/cloud.go
    blob: 64d1bb2852dc
  - path: source/toolkit/internal/cli/tests/cloud_init_test.go
    blob: 34753f6ef2ad
confidence: verified
---

The pre-run in `cli/root.go:173` starts the update check unless `cmd.Name() == "update"` or the command
carries `annotationSkipUpdateCheck` (`root.go:103`). `agtk cloud init` runs in every session-start hook
and must make no network call, but its leaf name equals top-level `agtk init`, so a name match would
exempt both. `cloud init` sets the annotation (`cli/cloud.go:45`).

`TestCloudInitStartsNoUpdateCheckWhileInitStillDoes` (`cli/tests/cloud_init_test.go:155`) asserts both
halves, observing via the `Env.StartUpdateCheck` seam. The `update` exemption is still a leaf-name
match, so any future subcommand named `update` skips the check too.
