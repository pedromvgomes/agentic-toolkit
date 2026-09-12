---
name: render-prunes-manifest-files-it-no-longer-owns
kind: invariant
description: Render deletes every path the previous manifest tracked that this render did not produce, so dropping a definition needs no cleanup step.
anchors:
  - path: source/toolkit/internal/adapters/claude/render.go
    blob: 2aefeb6f4588
  - path: source/toolkit/internal/adapters/codex/render.go
    blob: 7180daae7ec6
  - path: source/toolkit/internal/adapters/fsops/fsops.go
    blob: 5d3b80df26ed
confidence: verified
---

`Render` reads the previous manifest (`wholeOps.ReadManifest`,
`source/toolkit/internal/adapters/claude/render.go:106`), applies the current plan's whole-owned
ops into a fresh `newManifest` (`:126-131`), then diffs old against new
(`wholeOps.RemoveStale`, `:135`).

The diff itself is no longer adapter-local: it lives in the shared
`fsops.Ops.RemoveStale` (`source/toolkit/internal/adapters/fsops/fsops.go:180`), which walks
`oldManifest.Files`, skips anything the new manifest still tracks (`:189-192`), `os.Remove`s
the rest and reports `removed <path>` (`:208-212`). The codex adapter calls the same function
(`source/toolkit/internal/adapters/codex/render.go:130`), so the pruning rule is one
implementation shared by both renderers rather than a behaviour each one re-derives.

Consequence: removing a definition from the catalog and from every stack that listed it is
sufficient by itself — the consumer's next `agtk render` deletes the artifact. No cleanup
command exists, and none is needed.

Three limits. It fires only on a real render against a manifest that already tracked the file,
so a consumer who never re-renders keeps the stale artifact indefinitely. It reaches only
paths agtk owned: a file the user created out-of-band under `.claude/` is never touched, which
is the same manifest-membership rule that makes render refuse to overwrite it — see
[[render-refuses-files-it-does-not-track]]. And a manifest key that resolves *outside* the
render root is reported and left alone rather than deleted (`fsops.go:204-207`); the manifest
is a committed file, so its keys are untrusted input, and containment is decided after
`filepath.EvalSymlinks` on the entry's parent (`:194-203`) because a symlinked ancestor
escapes a purely lexical check just as `..` does.
