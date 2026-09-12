---
name: committed-content-cannot-live-in-a-rendered-tree
kind: rationale
description: Committed, hand-maintained files must sit outside every platform's rendered tree, which is why the review manifest and the memory store both moved out of .agents/ — but the shipped memory default still points inside it.
anchors:
  - path: .gitignore
    blob: f6d10425561d
  - path: .agentic-toolkit.yaml
    blob: 4b341b676e6c
  - path: source/toolkit/internal/review/manifest.go
    blob: b9f2e3c1afc2
  - path: source/toolkit/internal/memory/types.go
    blob: 112a6563285f
confidence: verified
---

A Platform adapter's output directory is regenerated and pruned by `agtk render`
(see [[render-prunes-manifest-files-it-no-longer-owns]]), so a repo ignores it
wholesale — `.gitignore:11-21` lists one blanket rule per rendered root
(`/.claude/`, `/CLAUDE.md`, `/.mcp.json`, `/.codex/`, `/AGENTS.md`, `/.agents/`).
Anything committed and hand-maintained placed inside one of those trees is
therefore reachable by the ignore rule that covers it, and the only alternatives
are an enumerated (leaky) ignore rule or moving the content out. This repo took
the second option on `chore/store-out-of-rendered-tree`, for both trees that had
been sitting under `.agents/`:

- The review manifest: `ManifestDir = ".agentic-toolkit/code-review"`
  (`source/toolkit/internal/review/manifest.go:14`), with the reason stated in its
  doc comment (`:9-13`) — "a repo that ignores its rendered trees wholesale would
  stop tracking the manifest, and an untracked manifest is unreadable at a ref".
  `LegacyManifestDir = ".agents/code-review"` (`:20`) is kept only so a manifest
  left behind there is refused rather than read as absent
  (`source/toolkit/internal/review/builtin.go:111-113`).
- The memory store: `.agentic-toolkit.yaml:36-38` sets `memory.root: .memory`,
  with the same reasoning in the comment above it (`:30-32`).

The gap this leaves: the shipped default was *not* changed. `DefaultRoot =
".agents/memory"` (`source/toolkit/internal/memory/types.go:27`) is what
`memory.New` applies when `memory.root` is unset
(`source/toolkit/internal/memory/store.go:31-35`), so a consumer adopting memory
without an explicit root still lands inside the codex adapter's tree. The trap is
documented rather than fixed — `docs/CONSUMER-GUIDE.md:211-215` tells anyone
rendering for `codex` to set `memory.root` outside `.agents/`, and notes the
failure is silent: the store keeps working because `agtk` reads the working tree,
while the notes stop being committed. Setting it is entry-manifest-only, see
[[memory-config-is-entry-manifest-only]].
