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
  - source/toolkit/internal/memory/search.go
  - docs/CONSUMER-GUIDE.md
  - docs/adr/0002-no-model-calls-in-agtk.md
---

Each item was read, not inferred, unless marked. `agtk memory search` is the worked example: it
went through every touchpoint below.

1. Registration: `newMemoryCmd` lists subcommands in `source/toolkit/internal/cli/memory.go`
   (`newMemorySearchCmd(env)` at `:52`). The group's unknown-subcommand guard (`Args: cobra.NoArgs`
   at `:43` + a `RunE` at `:44`) makes an older binary exit non-zero on an unknown subcommand, so a
   caller can probe with `agtk memory <cmd> --help >/dev/null 2>&1` (the pattern in
   `definitions/hooks/no-authoring-footers-claude-pre-tool-use.yaml:15`, per note
   hook-must-tolerate-an-older-agtk). The explorer's routing has no such probe: `AGENT.md` Step 2
   requires `search` to exist.
2. Grant: the pre-approved memory commands are `stats*`, `show *`, `search*` and `candidates*`
   (`definitions/settings/memory-permissions.yaml:7-12`). A command the explorer runs via Bash
   prompts on every delegation until it has a `Bash(agtk memory <cmd>*)` entry. The store-path
   `Read`/`Edit` grants are added by the claude adapter, not this file
   (`source/toolkit/internal/adapters/claude/settings.go`, the `Read(**/<root>/INDEX.md)` and
   `Edit(**/<root>/candidates/**)` rules); the `INDEX.md` read grant stays although the explorer no
   longer reads the index to route.
3. Tests that pin the grant list literally: `TestPreApprovedPermissionsNameCommandsThatExist`
   (`cli/tests/default_stack_render_test.go:168`, substring checks on rendered settings.json),
   `TestTheMemoryStackRendersOnItsOwn` (`cli/tests/memory_stack_render_test.go:22`) and
   `TestExtendingTheMemoryStackKeepsEveryDefaultGrant` (`:60`, exact-match `hasRule`), plus fixtures
   in `adapters/claude/tests/memory_grants_test.go` and `permissions_compose_test.go` that hard-code
   `Bash(agtk memory stats*)` (they need no change). The tests assert the presence of the grants in
   their want-lists, so a grant absent from the list is unprotected: removing the search grant from
   the yaml fails all three, and a grant never added to the lists fails nothing. A command that
   spends money stays unapproved: both files assert `curate` is absent from the allow list
   (`memory_stack_render_test.go:51`, `default_stack_render_test.go:203`).
4. Docs: the command block in `docs/CONSUMER-GUIDE.md` (`:326-335`) lists every subcommand,
   including `search` and `candidates`, and says every subcommand except `curate` is deterministic.
   `definitions/skills/using-agentic-toolkit/REFERENCE.md` has no memory subcommand list (only
   `memory:` config). ADR 0002 enumerates the deterministic surface as `index, anchor, audit, lint,
   show, stats, candidates` (`docs/adr/0002-no-model-calls-in-agtk.md:9`); it is prose, no test
   asserts it, and it does not name `search`.
5. The explorer's rules to keep in step: `AGENT.md` Step 2 (`:87`) runs `agtk memory search` and
   says not to read `INDEX.md` to route; Step 3 (`:108`) opens notes only through `show` because
   `show` records the hit and prints `stale:`, and search records neither a hit nor anything else.
   `INDEX.md`'s header line names `show` and `--no-hit` (`memory/index.go:37`, `RenderIndex`: "`agtk
   memory show <name>` opens one and records a hit; add `--no-hit` for a read that is not consulting
   the store."), and lint fails on any drift from `RenderIndex` (`Store.Lint` -> `IndexCurrent`), so
   changing that text needs `agtk memory index` re-run.
6. `lint` is two calls, not one. The `lint` command runs `store.Lint(notes, parseErrs)` and then
   appends `store.LintCandidates()` (`cli/memory.go:470,473`); `LintCandidates` is
   `memory/lint.go:117`. A check on candidates goes in `LintCandidates`, a check on notes or the
   index in `Store.Lint` (`memory/lint.go:53`), and the two are not interchangeable: `Store.Lint`
   also runs inside the curator's verification. The candidate-side test is
   `TestMemoryLintReportsAnUnreadableCandidate` in `cli/tests/memory_test.go`.
7. Path-to-anchor matching lives in `memory/search.go`: `coveringAnchors` (`:274`) and
   `anchorCovers` (`:298`). A concrete anchor covers the path it names; a glob anchor covers what
   `path.Match` says, one directory level; an anchor failing `ValidateAnchorPath` (`**`, absolute,
   `..`) covers nothing. `Anchor.Matches` (`memory/types.go:146`) is a field, the stamped expansion
   of a glob, and is not consulted. Notes come from `LoadNotes` sorted by name
   (`memory/store.go:140`, sort at `:170`), which search relies on for a stable order alongside its
   own name tie-break.
