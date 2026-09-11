---
name: settings-merge-is-shallow-last-wins
kind: gotcha
description: Two setting definitions writing the same top-level key resolve by stack order then definition name, silently and with no override diagnostic.
anchors:
  - path: internal/adapters/claude/settings.go
    blob: a76ce7243f96
  - path: internal/resolver/resolver.go
    blob: 022646e73709
confidence: verified
---

`collectSettingFragments` (`internal/adapters/claude/settings.go:263`) merges every `setting`
definition's `Value` into one map with a plain `for k, v := range c.Value { out[k] = v }`
(`:297-301`) — a shallow, whole-value overwrite per top-level key, not a deep merge. A
definition setting `permissions:` replaces another's `permissions:` entirely rather than
combining them.

Who wins is decided by the sort at `:290-295`: `plan.StackOrder` index first (depth-first
post-order, later index applied later), then definition name alphabetically as a tiebreak
within one stack. So two definitions in the same stack both writing `model:` resolve by whose
*name* sorts later — not by which is more specific.

And it is silent. The (category, name) override path in the resolver emits a `DiagOverride`
diagnostic (`internal/resolver/resolver.go:216`); nothing in settings collection notices that
two definitions touched the same key, so no warning is ever produced.
