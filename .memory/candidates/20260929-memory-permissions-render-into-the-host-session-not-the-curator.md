---
about: "definitions/settings/memory-permissions.yaml and the adapter's store-path grants reach the host session only; the headless curator never reads them"
saw:
  - definitions/settings/memory-permissions.yaml
  - source/toolkit/internal/adapters/claude/settings.go
  - source/toolkit/internal/curator/curator.go
  - docs/adr/0004-the-curator-ships-in-the-binary.md
  - source/toolkit/internal/cli/tests/memory_stack_render_test.go
  - source/toolkit/internal/cli/tests/default_stack_render_test.go
  - source/toolkit/internal/adapters/claude/tests/memory_grants_test.go
---

- `agtk memory curate` runs `claude -p` with `--setting-sources ""` (ADR 0004,
  `docs/adr/0004-the-curator-ships-in-the-binary.md:11-16`), so a consumer's rendered
  `.claude/settings.json` "pre-approved permissions" are out of its reach. The curator's authority
  is the argv `--allowedTools` built by `allowedTools` in `curator.go:311-374`. Adding
  `Bash(git rm ...)` to `memory-permissions.yaml` therefore changes what the explorer / main
  session may run without prompting and does not change what the curator may do.
- A store-path literal cannot go in the definition: the file's own comment
  (`memory-permissions.yaml:12-18`) says path grants depend on `memory.root`, which only the entry
  manifest sets. The adapter appends them: `addMemoryGrants` (`adapters/claude/settings.go:299`,
  doc `:285-298`) and `memoryGrants` (`:399-411`, doc `:383-398`), producing
  `Read(**/<root>/INDEX.md)` and `Edit(**/<root>/candidates/**)` (`:407-410`). They are appended
  only when `usesMemoryStore` (`:371`) holds and some definition already contributes
  `permissions.allow` (`:307-324`; a deny-only contribution gets none). A delete grant for
  candidates would belong there.
- Existing assertions are membership checks, not exact-list checks: `hasRule` in
  `cli/tests/memory_stack_render_test.go:43,75` (defined `:99-106`) and `strings.Contains` loops in
  `cli/tests/default_stack_render_test.go:175-183` and `:190-197`. Adding a grant breaks none of
  them; a new want would need adding. Adapter-level tests live in
  `adapters/claude/tests/memory_grants_test.go`.
- Docs that describe the grants: `docs/CONSUMER-GUIDE.md:318-323` ("pre-approved permissions for
  reading the index and staging candidates follow it"), and the `permissions` merge paragraph at
  `docs/CONSUMER-GUIDE.md:162-175`. `definitions/skills/using-agentic-toolkit/REFERENCE.md` has no
  permissions passage at all (grep `permission|Edit(` finds nothing).
- The explorer's `Edit(**/<root>/candidates/**)` grant covers Write/Edit but not deletion, and the
  curator's own candidate deletion is the argv `Bash(rm <candidatesDir>/*)` grant
  (`curator.go:362-364`), which no settings file can widen.
