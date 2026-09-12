---
name: settings-merge-is-shallow-last-wins
kind: gotcha
description: Two setting definitions writing the same top-level key resolve by stack order then definition name, silently and with no override diagnostic.
anchors:
  - path: source/toolkit/internal/adapters/claude/settings.go
    blob: a76ce7243f96
  - path: source/toolkit/internal/resolver/resolver.go
    blob: 022646e73709
confidence: verified
---

`collectSettingFragments` (`source/toolkit/internal/adapters/claude/settings.go:263`) merges every `setting`
definition's `Value` into one map with a plain `for k, v := range c.Value { out[k] = v }`
(`:297-301`) — a shallow, whole-value overwrite per top-level key, not a deep merge. A
definition setting `permissions:` replaces another's `permissions:` entirely rather than
combining them.

Who wins is decided by the sort at `:290-295`: `plan.StackOrder` index first (depth-first
post-order, later index applied later), then definition name alphabetically as a tiebreak
within one stack. So two definitions in the same stack both writing `model:` resolve by whose
*name* sorts later — not by which is more specific.

One key does not play by these rules. `hooks` is claimed by the hook renderer before setting
fragments are applied, and a setting contribution to an already-claimed key is dropped
outright (`settings.go:64-72`, `if managed[key] { continue }`) — so a `setting` definition
writing `hooks:` loses to rendered hooks regardless of stack order, which is the one place the
merge is first-wins rather than last-wins. Separately, every previously-managed key is deleted
before the merge (`:54-57`), so a key that stops being rendered does not linger.

And it is silent. The (category, name) override path in the resolver emits a `DiagOverride`
diagnostic (`source/toolkit/internal/resolver/resolver.go:216`); nothing in settings collection notices that
two definitions touched the same key, so no warning is ever produced.
