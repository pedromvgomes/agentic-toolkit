---
about: "definitions/settings/memory-permissions.yaml and the adapter's store-path grants reach the host session only; the headless curator never reads them"
saw:
  - definitions/settings/memory-permissions.yaml
  - source/toolkit/internal/adapters/claude/settings.go
  - docs/adr/0004-the-curator-ships-in-the-binary.md
  - source/toolkit/internal/cli/tests/memory_stack_render_test.go
  - source/toolkit/internal/cli/tests/default_stack_render_test.go
  - source/toolkit/internal/adapters/claude/tests/memory_grants_test.go
---

- `agtk memory curate` runs `claude -p` with `--setting-sources ""` (ADR 0004, lines 9-16), so a
  consumer's rendered `.claude/settings.json` "pre-approved permissions" are out of its reach. The
  curator's authority is the argv `--allowedTools` built in `curator.go:290-353`. Adding
  `Bash(git rm ...)` to `memory-permissions.yaml` therefore changes what the explorer / main
  session may run without prompting and does not change what the curator may do.
- A store-path literal cannot go in the definition: the file's own comment
  (`memory-permissions.yaml`, lines 12-19) says path grants depend on `memory.root`, which only the
  entry manifest sets. The adapter appends them: `addMemoryGrants` (`adapters/claude/settings.go:299`),
  `memoryGrants` (`:399-411`), producing `Read(**/<root>/INDEX.md)` and
  `Edit(**/<root>/candidates/**)`, gated on some definition already contributing `permissions.allow`
  (`:314-325`) and `usesMemoryStore` (`:356-`). A delete grant for candidates would belong there.
- Existing assertions are membership checks, not exact-list checks: `hasRule` in
  `cli/tests/memory_stack_render_test.go:33-45,64-70` and a `strings.Contains` loop in
  `default_stack_render_test.go:188-198`. Adding a grant breaks none of them; a new want would
  need adding. Adapter-level tests live in `adapters/claude/tests/memory_grants_test.go`.
- Docs that describe the grants: `docs/CONSUMER-GUIDE.md` lines ~318-323 ("pre-approved permissions
  for reading the index and staging candidates follow it"). `REFERENCE.md` has no permissions
  passage for the store (grep `permission|Edit(` -> only the `permissions` merge paragraph in the
  guide at `:162`).
- The existing explorer `Edit(candidates/**)` grant covers Write/Edit but not deletion.
