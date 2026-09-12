---
name: committed-content-cannot-live-in-a-rendered-tree
kind: rationale
description: Committed, hand-maintained files must sit outside every platform's rendered tree, which is why the review manifest and the memory store — including the shipped default root — sit outside .agents/.
anchors:
  - path: .gitignore
    blob: f6d10425561d
  - path: .agentic-toolkit.yaml
    blob: c9f158d61187
  - path: source/toolkit/internal/review/manifest.go
    blob: b720da152793
  - path: source/toolkit/internal/memory/types.go
    blob: 0a38918747bf
  - path: source/toolkit/internal/memory/store.go
    blob: 2324f6ddc151
confidence: verified
---

A Platform adapter's output directory is regenerated and pruned by `agtk render`
(see [[render-prunes-manifest-files-it-no-longer-owns]]), so a repo ignores it
wholesale — `.gitignore:11-21` lists one blanket rule per rendered root
(`/.claude/`, `/CLAUDE.md`, `/.mcp.json`, `/.codex/`, `/AGENTS.md`, `/.agents/`).
Anything committed and hand-maintained placed inside one of those trees is
therefore reachable by the ignore rule that covers it, and the only alternatives
are an enumerated (leaky) ignore rule or moving the content out. This repo takes
the second option for both trees that would otherwise sit under `.agents/`:

- The review manifest: `ManifestDir = ".agentic-toolkit/code-review"`
  (`source/toolkit/internal/review/manifest.go:14`), with the reason stated in its
  doc comment (`:9-13`) — "a repo that ignores its rendered trees wholesale would
  stop tracking the manifest, and an untracked manifest is unreadable at a ref".
  `LegacyManifestDir = ".agents/code-review"` (`:23`) is kept so a manifest left
  behind there is not read as absent. On disk that means a refusal
  (`legacyManifestErr`, `source/toolkit/internal/review/builtin.go:94-102`,
  called from `Load` at `:165`); at a ref it means the old path is read instead
  (`LoadAtRef`, `:129-137`), because `git mv` cannot reach history — see
  [[a-manifest-missing-at-the-ref-falls-back-silently]]. The const's own doc
  comment (`manifest.go:16-22`) states that split itself, so the two loaders'
  divergence is documented where the const is declared.
- The memory store: `DefaultRoot = ".memory"`
  (`source/toolkit/internal/memory/types.go:34`), with the same reasoning in the
  const's doc comment (`:28-33`) — the rule "binds this default as much as a path
  a consumer picks, because a consumer that sets nothing gets this one".
  `.agentic-toolkit.yaml:35-37` sets `memory.root: .memory` anyway, so this repo
  exercises the configured path as well as the default (`:30-31`).

`LegacyDefaultRoot = ".agents/memory"` (`types.go:39`) exists only so a store
scaffolded at that root is reported rather than read as a repo that never
adopted memory (`(*Store).CheckLegacyRoot`,
`source/toolkit/internal/memory/store.go:58-70`). Nothing loads from it
(`types.go:36-38`).

The store's refusal is deliberately narrower than the review manifest's. It fires only
when the root is not explicit, nothing exists at `.memory`, and a directory
exists at the legacy path (`store.go:59-65`), and its error names two remedies —
`git mv .agents/memory .memory`, or set `memory.root: .agents/memory`
(`store.go:66-69`). The review manifest's refusal fires regardless of
configuration, because its location is not configurable at all; a store's is, so
"stay put" is a real answer (`store.go:50-54`). Setting `memory.root` is
entry-manifest-only, see [[memory-config-is-entry-manifest-only]].
