---
name: render-refuses-files-it-does-not-track
kind: invariant
description: A file on disk that is absent from .agtk-manifest.json is treated as user-owned, and render refuses rather than overwrite it.
anchors:
  - path: internal/adapters/claude/render.go
    blob: 4a87de9b70a9
  - path: internal/adapters/claude/files.go
    blob: a62fd6986ab4
confidence: verified
---

Whole-owned outputs (skills, agents, commands, rules) are tracked in a sidecar manifest at
`<scope-root>/.agtk-manifest.json` (`internal/adapters/claude/files.go:297`). Membership in
that manifest is what grants agtk permission to overwrite: a path in it can be rewritten
freely, a path on disk but not in it is assumed to be the user's and render refuses unless
`Options.Force` is set (`internal/adapters/claude/render.go:114`).

The consequence that bites: deleting the manifest does not reset state, it *escalates* it —
every previously rendered file becomes a collision. Recovering means `--force`, not a
re-render.

Two other ownership models coexist under the same root and do not work this way: CLAUDE.md
is region-owned between markers, and settings.json / .mcp.json are key-owned via
`_meta.agtk.managed`.
