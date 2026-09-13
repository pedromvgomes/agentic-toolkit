---
about: `permissions` unions across settings definitions (allow/deny/ask); every other top-level settings key is still last-wins
saw:
  - source/toolkit/internal/adapters/claude/settings.go
  - source/toolkit/internal/adapters/claude/tests/permissions_compose_test.go
  - stacks/memory.yaml
  - stacks/default.yaml
  - definitions/settings/memory-permissions.yaml
  - definitions/settings/skill-permissions.yaml
  - source/toolkit/internal/cli/tests/default_stack_render_test.go
targets: settings-merge-is-shallow-last-wins
verdict: now-false
---

`collectSettingFragments` (settings.go:431-479) still does the shallow last-wins merge for
every top-level key EXCEPT `permissions` — for that one key it calls `composePermissions(out[k],
v, c.DefName)` (:467-473) instead of overwriting. `settings-merge-is-shallow-last-wins`
predates this and states the whole-key overwrite rule with no exception; it is now false for
`permissions` specifically and needs the carve-out stated, not just re-anchored.

`composePermissions` (settings.go:498-543) unions `allow`, `deny` and `ask`
(`permissionListKeys`, :547-551) member-by-member across every settings definition that
contributes `permissions`, deduplicated on the exact rule string (`containsGrant`, existing
helper) and ordered by the same `(StackOrder index, definition name)` sort `collectSettingFragments`
already used for last-wins keys — so composition is still reproducible and stack-order-driven,
just additive instead of destructive. Any other member of `permissions` a definition might set
(neither `allow`, `deny` nor `ask`) still last-wins inside the merged map (:516-518).

A member that isn't a list is refused rather than silently dropped or coerced:
`permissionList` (:558 on) returns an error naming the offending definition, and
`composePermissions` itself errors if `permissions` (or an existing accumulated value) isn't a
mapping at all (:499-509) — `renderSettings` propagates the error out of `Render` (settings.go:63-65,
`collectSettingFragments` now returns `(map[string]any, error)`), so a malformed contribution
fails the render rather than corrupting the pre-approval surface.

Narrowing is spelled `deny`, and this is now load-bearing rather than a style note: because
`allow` only ever grows via union, a settings definition can no longer take the `permissions`
key back from a stack it is layered with — the only way to restrict something another stack
allowed is to add it to `deny`, since Claude Code resolves `deny` ahead of `allow` (stated in
the composePermissions doc comment, settings.go:481-496).

Verified against the render: `stacks/memory.yaml` now owns `memory-permissions` (the three
`Bash(agtk memory ...)` grants) and `stacks/default.yaml` extends it while `skill-permissions`
keeps only the code-review and gh grants (`definitions/settings/skill-permissions.yaml`,
`definitions/settings/memory-permissions.yaml`). `TestPermissionsFromEveryStackSurviveTheMerge`
and `TestTheExtendingStackDoesNotEraseWhatItExtends` (permissions_compose_test.go) pin exactly
the case the old note said was impossible: a stack's own grants surviving being layered under
an extending stack that also contributes `permissions`. `TestPreApprovedPermissionsNameCommandsThatExist`
(default_stack_render_test.go) still finds every memory grant and both store-path grants in
`stacks/default.yaml`'s render — the regroup is set-identical for that stack, only `allow`'s
ordering changed (the extended stack, `memory.yaml`, now sorts first).

New consequence worth keeping regardless of the note: a consumer that extends only
`stacks/memory.yaml` (no `default.yaml`) now receives the `Read`/`Edit` store-path grants
(from `addMemoryGrants`, unchanged — settings.go's memory-root append logic) because
`memory-permissions` guarantees `permissions.allow` is non-empty for that consumer. Before
this change a memory-only stack had no settings definition of its own contributing
`permissions` at all, so `addMemoryGrants`'s existing-`allow`-list gate (it appends only when
some definition already contributed `permissions.allow`, never conjuring the key itself) meant
a memory-only consumer got no store-path grants and prompted on every delegation.
