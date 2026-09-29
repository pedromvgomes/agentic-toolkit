---
about: "what has to change together, and what tests assert it, when an agent-facing `agtk memory` subcommand is added or `lint` gains a check"
saw:
  - definitions/settings/memory-permissions.yaml
  - definitions/agents/memory-explorer/AGENT.md
  - source/toolkit/internal/cli/tests/default_stack_render_test.go
  - source/toolkit/internal/cli/tests/memory_stack_render_test.go
  - source/toolkit/internal/cli/memory.go
  - source/toolkit/internal/memory/index.go
  - source/toolkit/internal/memory/lint.go
  - docs/CONSUMER-GUIDE.md
  - docs/adr/0002-no-model-calls-in-agtk.md
---

Found while mapping a planned `agtk memory search`. Each item was read, not inferred, unless
marked.

1. Registration: `newMemoryCmd` lists subcommands in `source/toolkit/internal/cli/memory.go:46-55`.
   The group's unknown-subcommand guard (`:24-44`, `Args: cobra.NoArgs` at `:43` + a `RunE` at
   `:44`) makes an older binary exit non-zero on `agtk memory search`, so a caller can probe with
   `agtk memory search --help >/dev/null 2>&1` (the pattern in
   `definitions/hooks/no-authoring-footers-claude-pre-tool-use.yaml:15`, per note
   hook-must-tolerate-an-older-agtk).
2. Grant: only `stats*`, `show *` and `candidates*` are pre-approved
   (`definitions/settings/memory-permissions.yaml:7,8,11`). A new command the explorer runs via
   Bash prompts on every delegation until it has a `Bash(agtk memory <cmd>*)` entry. The
   store-path `Read`/`Edit` grants are added by the claude adapter, not this file
   (`source/toolkit/internal/adapters/claude/settings.go:285-299`).
3. Tests that pin the grant list literally: `TestPreApprovedPermissionsNameCommandsThatExist`
   (`cli/tests/default_stack_render_test.go:168-203`, substring checks on rendered settings.json),
   `TestTheMemoryStackRendersOnItsOwn` and `TestExtendingTheMemoryStackKeepsEveryDefaultGrant`
   (`cli/tests/memory_stack_render_test.go:22-79`, exact-match `hasRule`), plus fixtures in
   `adapters/claude/tests/memory_grants_test.go` and `permissions_compose_test.go` that hard-code
   `Bash(agtk memory stats*)` (they need no change, only if one wants search covered there).
   None fails by itself if a grant is simply forgotten: they assert presence of the existing
   three, so the new one must be added to the lists to be protected. A command that spends money
   stays unapproved: both files assert `curate` is absent from the allow list
   (`memory_stack_render_test.go:50`, `default_stack_render_test.go:201-203`).
4. Docs: `docs/CONSUMER-GUIDE.md:326-337` lists index/anchor/audit/lint/show/stats/curate and says
   "Nothing here calls a model except `curate`: `index`, `anchor`, `audit` and `lint` are
   deterministic"; `candidates` is absent, so the list is incomplete.
   `definitions/skills/using-agentic-toolkit/REFERENCE.md` has no memory subcommand list (only
   `memory:` config, ~:112,168). ADR 0002 enumerates the deterministic surface as
   `index, anchor, audit, lint, show, stats, candidates` (`docs/adr/0002-no-model-calls-in-agtk.md:9`);
   it is prose, no test asserts it.
5. The explorer's rules to keep in step: `AGENT.md` Step 2 (`:87-94`) says `Read` the INDEX
   (pre-approved `Read(**/<root>/INDEX.md)`), Step 3 (`:96-113`) says open notes only through
   `show` because `show` records the hit. `INDEX.md`'s header line names `show` and `--no-hit`
   (`memory/index.go:37`, `RenderIndex`: "`agtk memory show <name>` opens one and records a hit;
   add `--no-hit` for a read that is not consulting the store."), and lint fails on any drift from
   `RenderIndex` (`Store.Lint` -> `IndexCurrent`, `memory/lint.go:93-102`), so changing that text
   needs `agtk memory index` re-run.
6. `lint` is two calls, not one. The `lint` command runs `store.Lint(notes, parseErrs)` and then
   appends `store.LintCandidates()` (`cli/memory.go:469-472`); `LintCandidates` is
   `memory/lint.go:108-133`. A check on candidates goes in `LintCandidates`, a check on notes or
   the index in `Store.Lint`, and the two are not interchangeable: `Store.Lint` also runs inside
   the curator's verification. The candidate-side test is `cli/tests/memory_test.go:345`
   (`TestMemoryLintReportsAnUnreadableCandidate`); the docs that describe what `lint` covers are
   `docs/CONSUMER-GUIDE.md:329` and `:383-388`, the command's `Long` help (`cli/memory.go:453-462`)
   and `definitions/skills/open-pr/SKILL.md:89` (which runs it after staging).
7. No existing helper intersects a path with a note's anchors. `Anchor.IsGlob`
   (`memory/types.go:157`), `Anchor.Matches` (`types.go:146`, stamped expansion, stale until
   re-anchored) and `Store.expandGlob` (`memory/anchor.go:199`, live filesystem) are the pieces.
   `LoadNotes` returns notes sorted by name (`memory/store.go:140-172`, sort at `:170`), which is
   the only ordering guarantee today. Globs are one directory level (`**` rejected,
   `memory/lint.go:28-29`, ADR 0005, CONSUMER-GUIDE).
