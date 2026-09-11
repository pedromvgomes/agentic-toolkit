---
name: memory-new-does-not-validate-its-root
kind: invariant
description: memory.New accepts an absolute or climbing root; confinement lives in ValidateRoot, which only the CLI remembers to call.
anchors:
  - path: source/toolkit/internal/memory/store.go
    blob: 54fdd0d1f563
  - path: source/toolkit/internal/cli/memory.go
    blob: 555cb4f35496
  - path: source/toolkit/internal/memory/hits.go
    blob: 30d823466c1e
confidence: verified
---

`memory.New(projectRoot, root)` (`source/toolkit/internal/memory/store.go:31-40`) joins a relative root onto
`projectRoot` and takes an absolute root as-is. It checks nothing.

The confinement rule lives in `ValidateRoot` (`source/toolkit/internal/memory/store.go:46-57`), a free
function that rejects an absolute root and one climbing out with `..`. The reason is stated
at `:42-45`: the store is meant to be committed and to travel with its branch, and either
shape silently defeats both.

Exactly one caller pairs them, in the CLI: `source/toolkit/internal/cli/memory.go:134` runs
`ValidateRoot(root)` and only then `memory.New(...)` at `:137`. Nothing in the `memory`
package couples the two — `New`'s doc comment does not mention `ValidateRoot`, and no test in
`source/toolkit/internal/memory/tests/` asserts the pairing.

A second construction site — a new subcommand, a hook helper, the curator's own path
handling — that calls `memory.New` with a manifest-supplied `memory.root` and skips
`ValidateRoot` produces a Store rooted outside the repo, and everything follows: `Scaffold`
creates directories there, `WriteIndex` and `Stamp` write there, and `RecordHit`'s
`ensureGitignore` (`source/toolkit/internal/memory/hits.go:30`) creates the root as a side effect of a plain
read. The symptom is a store lint reports as green while the repo it is supposed to travel
with has nothing committed in it.

The durable fix, if this pairing is meant to hold, is folding the check into `New` and
returning an error, rather than remembering it at each call site.
