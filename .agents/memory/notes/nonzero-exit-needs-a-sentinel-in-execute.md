---
name: nonzero-exit-needs-a-sentinel-in-execute
kind: invariant
description: A command that prints its own report and exits non-zero must return a sentinel error registered in Execute.
anchors:
  - path: internal/cli/root.go
    blob: 2d489c31146f
  - path: internal/cli/status.go
    blob: 9c0513063466
  - path: internal/cli/memory.go
    blob: a594550f00b8
confidence: verified
---

`Execute` renders any error returned by a command through `renderTopLevelError`, prefixed
with `agtk:` (`internal/cli/root.go:250`). A command that has already printed a structured
report — drift buckets, stale notes, lint issues — would therefore print its findings
twice, the second time as an error message.

The protocol is a typed sentinel returned after printing, plus a branch in `Execute` that
maps it to an exit code and suppresses the prefix: `errStatusDrift` (`status.go:134`),
`errMemoryStale` / `errMemoryLint` (`memory.go`), and `updateNewerErr`, which maps to
`UpdateCheckExitCode` rather than 1.

Returning a plain `errors.New` from such a command is the failure: the exit code is right
and the output is wrong. `Execute` is also the only place that knows a sentinel exists, so
adding one without registering it there silently reverts to the prefixed rendering.
