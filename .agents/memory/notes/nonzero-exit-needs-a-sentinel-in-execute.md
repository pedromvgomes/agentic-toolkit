---
name: nonzero-exit-needs-a-sentinel-in-execute
kind: invariant
description: A command that prints its own report and exits non-zero must return a sentinel error registered in Execute.
anchors:
  - path: internal/cli/root.go
    blob: 87870af8e371
  - path: internal/cli/status.go
    blob: 9c0513063466
  - path: internal/cli/memory.go
    blob: c450efa1befe
confidence: verified
---

`Execute` renders any error returned by a command through `renderTopLevelError`, prefixed
with `agtk:` (called at `internal/cli/root.go:278`, defined at `:297`). A command that has
already printed a structured report — drift buckets, stale notes, lint issues, a curation
report — would therefore print its findings twice, the second time as an error message.

The protocol is a typed sentinel returned after printing, plus a branch in `Execute` that
maps it to an exit code and suppresses the prefix: `errStatusDrift` (declared
`status.go:134`, returned at `:117` and `:124`; branch at `root.go:256`), `errMemoryStale` /
`errMemoryLint` (declared `memory.go:63`/`:64`, returned at `:340`/`:422`; branch at
`root.go:261`), `errMemoryCurate` (declared `memory.go:68`, returned at `:749`; branch at
`root.go:267`), and `updateNewerErr`, which maps to `UpdateCheckExitCode` rather than 1
(branch at `root.go:274`).

`errMemoryCurate` is the same protocol applied to the one memory command that invokes a
model: `agtk memory curate` prints the curator's own report and returns the sentinel purely
to flip the exit code.

Returning a plain `errors.New` from such a command is the failure: the exit code is right
and the output is wrong. `Execute` is also the only place that knows a sentinel exists, so
adding one without registering it there silently reverts to the prefixed rendering.
