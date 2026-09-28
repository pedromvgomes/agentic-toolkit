---
about: everything the entry manifest contributes — scanned category files and the context: instruction alike — merges through the same per-category overlay loop as named stack entries, keyed by StackName = ""
saw:
  - source/toolkit/internal/resolver/resolver.go
  - source/toolkit/internal/resolver/entryscan.go
  - source/toolkit/internal/resolver/types.go
---

`traversalState.merge` (`resolver.go`) is the one place a `walkedDef` is written into
`s.overlay`; both `loadStack` (named stack entries) and `scanEntryRoot`/`scanCategory`
(`entryscan.go`, convention-scanned files) call it with a `walkedDef` carrying `StackName`.

The entry manifest's own contribution — everything under `<root>/<category>/` plus the
`context:` file — uses `StackName = ""`, the same identity a bare entry-manifest-level entry
already had before this branch's convention-scanning existed. `""` is appended to
`Plan.StackOrder` once, after `entry.Stacks` are walked (`entryscan.go`'s `loadEntry`), so local
content wins order-dependent `(category, name)` collisions by construction — no invented
`StackName` (like `"local.<category>"`) and no special append-position logic exist anywhere in
this code path. `PlannedDefinition.IsContext` (`types.go`) is the only marker that distinguishes
the `context:`-derived instruction from everything else sharing `StackName = ""`.
