---
about: a definition's StackName must be a member of Plan.StackOrder, or it silently loses every order-dependent adapter merge — an unrecognized name indexes to -1 and sorts first, not last
saw:
  - source/toolkit/internal/adapters/claude/settings.go
  - source/toolkit/internal/adapters/codex/config.go
  - source/toolkit/internal/resolver/local.go
  - source/toolkit/internal/resolver/resolver.go
---

`renderSettings` (Claude, `settings.go`) and the Codex config merge (`config.go`) both resolve
merge precedence by building `stackIdx := map[string]int{}` from `plan.StackOrder` and looking
up each contributing definition's `d.StackName` in it. A `StackName` that is not a member of
`plan.StackOrder` maps to Go's zero value for a missing map key via the `ok` idiom, and both
call sites treat that as `idx = -1` — which sorts **first** in the merge order, not last. A
definition whose author intends it to win ("this should override everything else") loses to
literally every recognized stack if its `StackName` was never appended to `StackOrder`.

This was a real bug caught by plan review before `local:` scanning shipped: an early draft gave
locally-scanned definitions an arbitrary `StackName` literal (e.g. `"local"`) without appending
it to `Plan.StackOrder`, so a locally-scanned setting or config value would have *lost* the
override it was designed to win. The fix (`resolver/local.go`, `scanLocal`) is to give each
locally-scanned definition `StackName = "local.<category>"` and append that identifier to
`Plan.StackOrder` — after the entry manifest's own `""` — for every category whose scan actually
produced a definition. `StackOrder` order is `sort.SliceStable`'s tie-break for
`stackIdx`-driven merges (`stackIdx[d.StackName]`, later index = applied later = wins), so
appending last is what makes "local wins" literally true rather than aspirational.

The general lesson: any code introducing a new definition-producing path (a new scan mechanism,
a synthetic definition, anything that doesn't go through the normal per-stack `loadStack` walk)
must audit every consumer of `StackName`/`Plan.StackOrder` — not just where the definition is
merged into the overlay, but every adapter that later indexes by that string — because an
identifier absent from `StackOrder` fails silently rather than erroring.
