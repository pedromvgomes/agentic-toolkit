---
about: what has to change together, and what tests assert it, when an agent-facing `agtk memory` subcommand is added
saw:
  - definitions/settings/memory-permissions.yaml
  - definitions/agents/memory-explorer/AGENT.md
  - source/toolkit/internal/cli/tests/default_stack_render_test.go
  - source/toolkit/internal/cli/tests/memory_stack_render_test.go
  - source/toolkit/internal/cli/memory.go
  - docs/CONSUMER-GUIDE.md
  - docs/adr/0002-no-model-calls-in-agtk.md
---

Found while mapping a planned `agtk memory search`. Each item was read, not inferred, unless
marked.

1. Registration: `newMemoryCmd` lists subcommands in `source/toolkit/internal/cli/memory.go:46-55`.
   The group's unknown-subcommand guard (`:33-44`, `Args: cobra.NoArgs` + a `RunE`) makes an
   older binary exit non-zero on `agtk memory search`, so a caller can probe with
   `agtk memory search --help >/dev/null 2>&1` (the pattern in
   `definitions/hooks/no-authoring-footers-claude-pre-tool-use.yaml`, per note
   hook-must-tolerate-an-older-agtk).
2. Grant: only `stats*`, `show *` and `candidates*` are pre-approved
   (`definitions/settings/memory-permissions.yaml:7-11`). A new command the explorer runs via
   Bash prompts on every delegation until it has a `Bash(agtk memory <cmd>*)` entry. The
   store-path `Read`/`Edit` grants are added by the claude adapter, not this file
   (`source/toolkit/internal/adapters/claude/settings.go:285-299`).
3. Tests that pin the grant list literally: `TestPreApprovedPermissionsNameCommandsThatExist`
   (`cli/tests/default_stack_render_test.go:168-183`, substring checks on rendered settings.json),
   `TestTheMemoryStackRendersOnItsOwn` and `TestExtendingTheMemoryStackKeepsEveryDefaultGrant`
   (`cli/tests/memory_stack_render_test.go:22-79`, exact-match `hasRule`), plus fixtures in
   `adapters/claude/tests/memory_grants_test.go` and `permissions_compose_test.go` that hard-code
   `Bash(agtk memory stats*)` (they need no change, only if one wants search covered there).
   None fails by itself if a grant is simply forgotten: they assert presence of the existing
   three, so the new one must be added to the lists to be protected.
4. Docs: `docs/CONSUMER-GUIDE.md:325-336` lists only index/anchor/audit/lint/show/stats and says
   "`index`, `anchor`, `audit` and `lint` are deterministic"; `candidates` and `curate` are already
   absent, so the list is already incomplete. `definitions/skills/using-agentic-toolkit/REFERENCE.md`
   has no memory subcommand list (only memory: config at ~:112,168). ADR 0002 enumerates the
   deterministic surface as `index, anchor, audit, lint, show, stats, candidates`
   (`docs/adr/0002-no-model-calls-in-agtk.md`, 3rd paragraph); it is prose, no test asserts it.
5. The explorer's rules to keep in step: AGENT.md Step 2 says `Read` the INDEX (pre-approved
   `Read(**/<root>/INDEX.md)`), Step 3 says open notes only through `show` because `show` records
   the hit. `INDEX.md` header also names `show` (`memory/index.go:14-24`,
   `RenderIndex` line "Read one with `agtk memory show <name>`") and lint fails on any drift
   from `RenderIndex`, so changing that text needs `agtk memory index` re-run.
6. No existing helper intersects a path with a note's anchors. `Anchor.IsGlob`
   (`memory/types.go`), `Anchor.Matches` (stamped expansion, stale until re-anchored) and
   `Store.expandGlob` (`memory/anchor.go:199`, live filesystem) are the pieces. `LoadNotes`
   returns notes sorted by name (`memory/store.go:140-172`), which is the only ordering
   guarantee today. Globs are one directory level (`**` rejected, ADR 0005 / CONSUMER-GUIDE).
