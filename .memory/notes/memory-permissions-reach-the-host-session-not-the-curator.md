---
name: memory-permissions-reach-the-host-session-not-the-curator
kind: gotcha
description: memory-permissions.yaml and the adapter's store-path grants apply to the host session and explorer only; the headless curator runs with --setting-sources empty and takes its grants from argv.
anchors:
  - path: definitions/settings/memory-permissions.yaml
    blob: 0a687f250bde
  - path: source/toolkit/internal/adapters/claude/settings.go
    blob: c5a0bf32cef5
  - path: source/toolkit/internal/curator/curator.go
    blob: cb07c4d5ea3f
  - path: docs/adr/0004-the-curator-ships-in-the-binary.md
    blob: 123f8eae6042
  - path: docs/CONSUMER-GUIDE.md
    blob: f5bab6fa9765
confidence: verified
---

- `agtk memory curate` runs `claude -p` with `--setting-sources ""` (ADR 0004,
  `docs/adr/0004-the-curator-ships-in-the-binary.md:11-16`), so a consumer's rendered
  `.claude/settings.json` is out of its reach. Its authority is the argv `--allowedTools` from
  `allowedTools` (`curator.go:311-374`). Adding `Bash(git rm ...)` to `memory-permissions.yaml`
  changes what the explorer/main session may run unprompted, not what the curator may do.
- A store-path literal cannot go in the definition: path grants depend on `memory.root`, which only
  the entry manifest sets (`memory-permissions.yaml:12-18`). The adapter appends them:
  `addMemoryGrants` (`adapters/claude/settings.go:299`) and `memoryGrants` (`:399-411`), giving
  `Read(**/<root>/INDEX.md)` and `Edit(**/<root>/candidates/**)` (`:407-410`), only when
  `usesMemoryStore` (`:371`) holds and some definition already contributes `permissions.allow`
  (`:307-324`; a deny-only contribution gets none). A delete grant for candidates would belong there.
- The explorer's `Edit(**/<root>/candidates/**)` covers Write/Edit, not deletion; the curator's
  deletion is the argv `Bash(rm <candidatesDir>/*)` (`curator.go:362-364`).
- Existing tests are membership checks: `hasRule` in `cli/tests/memory_stack_render_test.go:43,75`
  (defined `:99-106`) and `strings.Contains` loops in `cli/tests/default_stack_render_test.go:175-197`,
  so adding a grant breaks none; a new want must be added. Adapter tests:
  `adapters/claude/tests/memory_grants_test.go`.
- Docs: `docs/CONSUMER-GUIDE.md:318-323` and the `permissions` merge paragraph `:162-175`;
  `definitions/skills/using-agentic-toolkit/REFERENCE.md` has no permissions passage.
See also [[new-memory-subcommand-touchpoints]].
