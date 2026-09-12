---
about: memory.DefaultRoot moved to .memory; the note's "gap" paragraph is now false and the legacy path gets its own refusal, narrower than the review manifest's
saw:
  - source/toolkit/internal/memory/types.go
  - source/toolkit/internal/memory/store.go
  - source/toolkit/internal/cli/memory.go
  - source/toolkit/internal/stack/types.go
  - source/toolkit/internal/stack/tests/parser_test.go
  - definitions/settings/skill-permissions.yaml
  - source/toolkit/internal/cli/tests/default_stack_render_test.go
  - docs/CONSUMER-GUIDE.md
  - CONTEXT.md
targets: committed-content-cannot-live-in-a-rendered-tree
verdict: now-false
---

Re-checked on `fix/memory-default-out-of-rendered-tree`, uncommitted. Supersedes the earlier
candidate `20260912-memory-default-still-inside-agents-legacy-refusal-is-narrower.md`
(removed) — that one re-verified the note against the pre-move tree and flagged one stale
pointer; this candidate is against the post-move tree and the note's central claim itself no
longer holds.

**What is now false.** The note's last paragraph — "the shipped default was *not* changed...
`DefaultRoot = ".agents/memory"`... a consumer adopting memory without an explicit root still
lands inside the codex adapter's tree" — is no longer true. `memory.DefaultRoot` is now
`".memory"` (`source/toolkit/internal/memory/types.go:32`, previously `:27`). A new
`LegacyDefaultRoot = ".agents/memory"` const (`types.go:37`) exists solely so the *old*
location is recognized, not read from.

**What survives unchanged.** The rest of the note's mechanism is intact: `.gitignore`'s
blanket per-rendered-root rules, the review manifest's move to
`.agentic-toolkit/code-review` with its `LegacyManifestDir` kept for refusal
(`review/manifest.go:14,20`), and the general shape "committed+hand-maintained must sit
outside every platform's rendered tree." `[[memory-config-is-entry-manifest-only]]` cross-ref
is unaffected.

**The new legacy-refusal mechanism (answers "what would a future reader re-derive").**
`(*Store).CheckLegacyRoot()` (`memory/store.go:57-71`) refuses only when all three hold:

1. `!s.RootIsExplicit` — `memory.root` was never set in the entry manifest;
2. `!s.Exists()` — nothing is scaffolded at the *new* default (`.memory`);
3. a directory exists at `LegacyDefaultRoot` (`.agents/memory`) relative to `ProjectRoot`.

All three matter and each narrows the check on purpose:
- Condition 1 is why an explicitly configured `memory.root: .agents/memory` is *never*
  second-guessed: "the rule that moved this default binds what agtk fixes, not what a
  consumer picks" (`store.go:49-51`). This is the opposite of the review manifest's
  `legacyManifestErr`, which fires regardless of configuration because the review manifest's
  location is not configurable at all.
- Condition 2 means a repo that *did* migrate (moved its notes to `.memory`) but left an
  empty or stale `.agents/memory/` directory behind is not nagged — presence of the new store
  wins over presence of the old one.
- Condition 3 is why a repo that never adopted memory at all (no store anywhere) is silent:
  `CheckLegacyRoot` returns nil, and the caller's normal "not adopted yet" path runs
  (`cli/memory.go:141`, called from `memoryStore`).

The error names both remedies — `git mv .agents/memory .memory`, or set
`memory.root: .agents/memory` to leave it there (`store.go:65-68`) — mirroring
`legacyManifestErr`'s two-remedy shape, but for a genuinely configurable location rather than
a fixed one.

**No ref-shaped reader, and the doc comment says why.** `store.go:54-55` states directly:
"Nothing reads a store at a git ref, so unlike the review manifest there is no history-shaped
caller that has to read the old path instead of refusing." So there is no
`LoadAtRef`-style split for the memory store (the working-tree-only vs. ref-reading
asymmetry the review manifest has) — `CheckLegacyRoot` is the whole story.

**Two drift sites, held together by a test, not the compiler.** The literal default also
appears in the `agtkdoc` struct tag on `stack.MemoryConfig.Root` (`stack/types.go:76`,
`agtkdoc:"...Defaults to \".memory\"."`) — a struct tag cannot interpolate a Go const, so
this is a second, independent place the value `.memory` is spelled out. `TestMemoryRootDocNamesTheRealDefault`
(`stack/tests/parser_test.go:316-324`) asserts the tag's text contains
`"` + `memory.DefaultRoot` + `"`, i.e. it fails at compile-and-test time if the two diverge —
but nothing stops a future edit to one from shipping without the other except this test
running. `definitions/CONFIG-SCHEMA.md` (generated from that tag) is the third place the
string appears, downstream of both, and per `[[generated-schema-docs-have-no-ci-guard]]`
nothing in CI regenerates or diffs it.

**Third drift site: rendered settings grants.** `definitions/settings/skill-permissions.yaml`
now grants `Read(**/.memory/INDEX.md)` and `Write(**/.memory/candidates/**)`
(lines 17-18) — literal strings, same as before the move, with the comment above them now
explicit about why they can't be derived: "a settings definition carries an opaque value and
the adapter that merges it never resolves the manifest" (lines 11-16, consistent with
`[[settings-merge-is-shallow-last-wins]]`). A new assertion in
`cli/tests/default_stack_render_test.go:123-128` pins the two grant strings to
`"Read(**/" + memory.DefaultRoot + "/INDEX.md)"` etc., built from the Go const at test-compile
time — so this drift site, unlike the struct-tag one, cannot silently diverge from
`memory.DefaultRoot` in a passing build. It remains uncorrelated with a consumer's own
`memory.root`, by explicit design ("carried debt" per the coordinator): a consumer that
configures a non-default root still needs its own grant entries, unchanged from before.

**Docs pointer moved.** `docs/CONSUMER-GUIDE.md`'s memory-store section is now at line 208
(`## Memory store`, previously cited as `:211-215` under the old numbering scheme — the
section header itself shifted, not just the interior lines) and states the new default,
the "set memory.root, keep it outside every rendered tree" guidance now pointing at
CONTEXT.md's **Toolkit namespace** entry, and the legacy-store paragraph (lines 218-220)
describing exactly the same two remedies `CheckLegacyRoot`'s error offers.
