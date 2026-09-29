---
name: new-memory-subcommand-touchpoints
kind: gotcha
description: Adding an agent-facing `agtk memory` subcommand or a lint check touches registration, the permission yaml, three literal-grant tests, docs, the explorer prompt and the INDEX header; a grant left out of the test lists is unprotected.
anchors:
  - path: definitions/settings/memory-permissions.yaml
    blob: 0a687f250bde
  - path: definitions/agents/memory-explorer/AGENT.md
    blob: 02e85111f1fc
  - path: source/toolkit/internal/cli/memory.go
    blob: ac2b4d13579f
  - path: source/toolkit/internal/memory/index.go
    blob: 44e4ee0ed4cd
  - path: source/toolkit/internal/memory/lint.go
    blob: 69162c090427
  - path: source/toolkit/internal/memory/search.go
    blob: 364ef0dd41c4
  - path: docs/CONSUMER-GUIDE.md
    blob: f5bab6fa9765
  - path: docs/adr/0002-no-model-calls-in-agtk.md
    blob: 82d213ee7ac8
confidence: verified
---

`agtk memory search` went through every touchpoint:

1. Registration: `newMemoryCmd` in `cli/memory.go` (`newMemorySearchCmd(env)` at `:52`). The group's
   `Args: cobra.NoArgs` (`:43`) plus `RunE` (`:44`) makes an older binary exit non-zero on an unknown
   subcommand, so callers can probe with `agtk memory <cmd> --help >/dev/null 2>&1` (see
   [[hook-must-tolerate-an-older-agtk]]). The explorer has no probe: `AGENT.md` Step 2 requires
   `search`.
2. Grant: pre-approved commands are `stats*`, `show *`, `search*`, `candidates*`
   (`memory-permissions.yaml:7-12`); a new command prompts on every delegation until it has a
   `Bash(agtk memory <cmd>*)` entry. See [[memory-permissions-reach-the-host-session-not-the-curator]].
3. Tests pinning the list literally: `TestPreApprovedPermissionsNameCommandsThatExist`
   (`cli/tests/default_stack_render_test.go:168`), `TestTheMemoryStackRendersOnItsOwn`
   (`cli/tests/memory_stack_render_test.go:22`), `TestExtendingTheMemoryStackKeepsEveryDefaultGrant`
   (`:60`). Removing the search grant fails all three; a grant never added to their want-lists fails
   nothing. `curate` (spends money) must stay absent (`memory_stack_render_test.go:51`,
   `default_stack_render_test.go:203`).
4. Docs: `docs/CONSUMER-GUIDE.md:326-335` lists every subcommand and says all but `curate` are
   deterministic. ADR 0002 enumerates the deterministic surface as `index, anchor, audit, lint, show,
   stats, candidates` (`docs/adr/0002-no-model-calls-in-agtk.md:9`); prose, untested, omits `search`.
5. Explorer rules: `AGENT.md` Step 2 (`:87`) runs `search` and says not to read `INDEX.md` to route;
   Step 3 (`:108`) opens notes only via `show` (records the hit, prints `stale:`); search records
   neither. The `INDEX.md` header (`memory/index.go:37`) drift-checked by lint, so edit it then run
   `agtk memory index`.
6. `lint` is two calls: `store.Lint(notes, parseErrs)` then `store.LintCandidates()`
   (`cli/memory.go:470,473`; `memory/lint.go:53`, `:117`). Candidate checks go in `LintCandidates`,
   note/index checks in `Store.Lint`, which also runs inside curator verification. Test:
   `TestMemoryLintReportsAnUnreadableCandidate` (`cli/tests/memory_test.go`).
7. Search's path-to-anchor matching: `coveringAnchors` (`memory/search.go:274`), `anchorCovers`
   (`:298`). A glob anchor covers what `path.Match` says (one directory level); an anchor failing
   `ValidateAnchorPath` covers nothing; `Anchor.Matches` (`memory/types.go:146`) is not consulted.
   Notes load sorted by name (`memory/store.go:140`, sort `:170`), which search relies on.
