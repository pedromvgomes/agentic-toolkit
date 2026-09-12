---
name: render-refuses-files-it-does-not-track
kind: invariant
description: A file on disk that is absent from .agtk-manifest.json is treated as user-owned, and render refuses rather than overwrite it.
anchors:
  - path: source/toolkit/internal/adapters/fsops/fsops.go
    blob: 5d3b80df26ed
  - path: source/toolkit/internal/adapters/claude/render.go
    blob: 2aefeb6f4588
confidence: verified
---

Whole-owned outputs (skills, agents, commands, rules) are tracked in a sidecar manifest whose
fixed name is `ManifestFileName = ".agtk-manifest.json"`
(`source/toolkit/internal/adapters/fsops/fsops.go:267`), written under the tracked root.
Membership in that manifest is what grants agtk permission to overwrite: `DetectCollisions`
(`fsops.go:127-137`) skips any op whose `RelPath` is already in `manifest.Files` and errors on
any op whose `AbsPath` exists on disk but is untracked — "exists and is not tracked by agtk;
rerun with --force to overwrite". The refusal is gated on `!opts.Force`
(`source/toolkit/internal/adapters/claude/render.go:111`).

The consequence that bites: deleting the manifest does not reset state, it *escalates* it —
every previously rendered file becomes a collision. Recovering means `--force`, not a
re-render.

This machinery is **not** claude-specific. It lives in `internal/adapters/fsops` and is shared
by every platform adapter (`fsops.go:1-10`); an adapter supplies only its error prefix via
`fsops.New(prefix)` (`fsops.go:48`) plus its category-to-directory mapping. The codex adapter
tracks against the same manifest under `<ProjectRoot>/.agents/`
(`source/toolkit/internal/adapters/codex/render.go:11`). A claim about "claude's manifest" is
really a claim about all of them.

Two other ownership models coexist under the same scope root and do not work this way:
CLAUDE.md is region-owned between `<!-- BEGIN AGTK MANAGED -->` / `<!-- END AGTK MANAGED -->`
markers, and settings.json / .mcp.json are key-owned via `_meta.agtk.managed`. All three are
laid out in the package doc at `source/toolkit/internal/adapters/claude/render.go:4-26`.
