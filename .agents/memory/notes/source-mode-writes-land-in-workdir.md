---
name: source-mode-writes-land-in-workdir
kind: invariant
description: Under --source the toolkit tree is read-only; every write goes to the working directory instead.
anchors:
  - path: internal/cli/paths.go
    blob: d30919b87867
  - path: internal/cli/memory.go
    blob: c450efa1befe
confidence: verified
---

`--source` applies a toolkit tree from elsewhere on disk as if agtk were run there, but
that tree usually belongs to someone else and must stay untouched. So the read root and
the write root diverge: `stackDir` returns `SourceDir` (`internal/cli/paths.go:42`) while
`lockfilePath` returns `WorkDir` (`paths.go:71`), and the memory store follows the same
split through `memoryProjectRoot` (`internal/cli/memory.go:77`).

Any new command that writes has to make this choice explicitly — there is no default that
is right for both modes, and getting it wrong writes into a shared source tree without
saying so. `memory` also reads its `memory.root` from the *consumer's* manifest rather than
the source's for the same reason (`memoryManifestPath`, `memory.go:90`): where a repo
commits its notes is not a shared tree's decision.
See [[nonzero-exit-needs-a-sentinel-in-execute]] for the other cross-command
protocol a new subcommand has to opt into.
