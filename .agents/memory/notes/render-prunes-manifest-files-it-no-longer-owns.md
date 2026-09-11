---
name: render-prunes-manifest-files-it-no-longer-owns
kind: invariant
description: Render deletes every path the previous manifest tracked that this render did not produce, so dropping a definition needs no cleanup step.
anchors:
  - path: source/toolkit/internal/adapters/claude/render.go
    blob: 4a87de9b70a9
confidence: verified
---

`Render` reads the previous manifest (`readManifest`,
`source/toolkit/internal/adapters/claude/render.go:109`), applies the current plan's whole-owned ops into a
fresh `newManifest` (`:130-135`), then diffs: every path in the *old* manifest absent from the
new one is `os.Remove`d, reporting `removed <path>` (`:139-150`).

Consequence: removing a definition from the catalog and from every stack that listed it is
sufficient by itself — the consumer's next `agtk render` deletes the artifact. No cleanup
command exists, and none is needed.

Two limits. It fires only on a real render against a manifest that already tracked the file,
so a consumer who never re-renders keeps the stale artifact indefinitely. And it reaches only
paths agtk owned: a file the user created out-of-band under `.claude/` is never touched, which
is the same manifest-membership rule that makes render refuse to overwrite it — see
[[render-refuses-files-it-does-not-track]].
