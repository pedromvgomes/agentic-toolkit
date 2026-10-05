---
about: "Re-check of stale CLI notes: only-lock-resolves-refs and user-config-errors-are-swallowed-by-root still hold; Execute sentinel branches moved"
saw:
  - source/toolkit/internal/cli/root.go
  - source/toolkit/internal/cli/render.go
  - source/toolkit/internal/cli/sync.go
targets: only-lock-resolves-refs
verdict: still-true
---
`grep NewLiveProvider|NewFrozenProvider source/toolkit/internal/cli/*.go`: Live only at lock.go:57 and
sync.go:72; Frozen at fetch.go:42, plan.go:54, render.go:70, status.go:81, sync.go:92 (line numbers drifted by
1-2 from the note). `userconfig.Load` still discarded at root.go:187 (same line, still-true for
user-config-errors-are-swallowed-by-root). In `Execute`, sentinel branches are now at root.go:~264-284
(`errStatusDrift`, `errMemoryStale/Lint`, `errMemoryCurate`, `errMemoryUnfolded`); a new command that prints
its own report must add one (nonzero-exit-needs-a-sentinel-in-execute, still-true; its line numbers are old).
New top-level commands register in the `root.AddCommand(...)` list (root.go:171-175); `newGuardCmd` is the
smallest analog (guard.go).
