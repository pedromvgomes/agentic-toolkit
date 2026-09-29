---
name: settings-merge-is-shallow-last-wins
kind: gotcha
description: Setting definitions merge shallow last-wins per top-level key by stack order then name, silently; only `permissions` is unioned (allow/deny/ask), so narrowing must be spelled deny.
anchors:
  - path: source/toolkit/internal/adapters/claude/settings.go
    blob: c5a0bf32cef5
  - path: source/toolkit/internal/resolver/resolver.go
    blob: 74ae65027f11
confidence: verified
---

`collectSettingFragments` (`source/toolkit/internal/adapters/claude/settings.go:431`) merges every
`setting` definition's `Value` into one map. For every top-level key except `permissions` it is
a plain whole-value overwrite (`out[k] = v`, `:475`), not a deep merge: two definitions writing
`model:` resolve by the sort at `:458-463` (`plan.StackOrder` index first, then definition name
alphabetically), so within one stack the later *name* wins, not the more specific one.

`permissions` is the exception. `:467-473` calls `composePermissions` (`:498`), which unions
`allow`, `deny` and `ask` (`permissionListKeys`, `:547`) member by member, deduplicated on the
exact rule string and ordered by the same sort, so a render stays reproducible. Any other
member of `permissions` is still last-wins (`:516-518`). A non-list member, or a non-mapping
`permissions`, is an error naming the definition (`permissionList`, `:558`), which propagates out
of `renderSettings` (`:61-64`) rather than dropping grants.

Consequence: because `allow` only grows, a definition cannot take `permissions` back from a
stack it is layered with. The only way to restrict something another stack allowed is to add
it to `deny`, which Claude Code resolves ahead of `allow` (doc comment at `:481-497`). Also,
`addMemoryGrants` (`:299`) appends the store-path `Read`/`Edit` grants only when some
definition already contributed `permissions.allow`; `memory-permissions.yaml` guarantees that
for a consumer extending only `stacks/memory.yaml`.

`hooks` is claimed by the hook renderer first, and a setting contribution to an already-claimed
key is dropped (`:91-96`, `if managed[key] { continue }`), the one first-wins case. Previously
managed keys are deleted before the merge (`:79-84`), so a key that stops being rendered does
not linger.

It is silent for every key: the (category, name) override path emits a `DiagOverride`
(`source/toolkit/internal/resolver/resolver.go:263`), but nothing in settings collection
notices two definitions touching the same key.
