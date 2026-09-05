---
about: the sentinel invariant still holds and has a fourth instance, but its root.go pointer moved
saw:
  - internal/cli/root.go
  - internal/cli/memory.go
targets: nonzero-exit-needs-a-sentinel-in-execute
verdict: still-true
---

Re-checked because the note went stale: `root.go` and `memory.go` both changed.

The claim holds. `Execute` still renders a command's error through `renderTopLevelError`
with the `agtk:` prefix, and still branches on each sentinel to suppress it.

Pointer correction: the note says `internal/cli/root.go:272`; `renderTopLevelError` is now
called at `:278` and defined at `:284`. The other pointer is accurate —
`internal/cli/status.go:134` is `return errStatusDrift`.

There is now a fourth sentinel, and it is worth naming in the body alongside the others:
`errMemoryCurate` (`internal/cli/memory.go`), registered at `internal/cli/root.go:267`.
`agtk memory curate` prints the curator's own report and returns it purely to flip the exit
code — the same protocol, for the one memory command that invokes a model.
