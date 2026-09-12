---
name: two-committed-trees-live-under-dot-agents
kind: gotcha
description: A blanket /.agents/ gitignore rule would stop tracking .agents/code-review/manifest.yaml as well as the memory store, and the rule's own comment names only the store.
anchors:
  - path: .gitignore
    blob: d9ccabf78f17
  - path: source/toolkit/internal/review/manifest.go
    blob: fcd5b096bbb5
  - path: source/toolkit/internal/review/builtin.go
    blob: ad571de35ce9
confidence: verified
---

`.gitignore` ignores the rendered tree under `.agents/` entry by entry (`.gitignore:24-25`:
`/.agents/skills/`, `/.agents/.agtk-manifest.json`) rather than with a blanket `/.agents/`,
and the comment saying why (`.gitignore:21-23`) names one reason: "the memory store lives
under `.agents/memory` and is committed". It misses the second committed, hand-maintained
tree in the same directory — `.agents/code-review/manifest.yaml`.

That path is not rendered and not configurable: `ManifestDir = ".agents/code-review"` is a
constant (`source/toolkit/internal/review/manifest.go:8`) with `ManifestFile =
"manifest.yaml"` (`:11`), and nothing under `source/toolkit/internal/adapters/` mentions
either (grep returns nothing), so `agtk render` never writes it — the only writer is
`agtk code-review init`.

It has to stay tracked because the manifest is read out of git, not off disk. `LoadAtRef`
(`source/toolkit/internal/review/builtin.go:76`) reads `<base ref>:.agents/code-review/manifest.yaml`
(`ManifestRelPath`, `:68`) with `git show` (`:95`) — at the base ref, never the head, so a
branch cannot name the reviewers that judge it (`:71-75`).

The failure mode is silent in both directions. A blanket rule does not untrack an
already-committed file, so edits after it stop landing without `git add -f` and `git status`
omits them — the review keeps running against the last committed version. And a manifest
missing from the ref is not an error: `cat-file -e` failing at `:88` is read as "this repo
declares no manifest at the base" and the review falls back to the embedded default roster
(`:89-92`), with nothing in the output saying the repo's own rules were not used.

The general shape: `/.agents/` is a shared parent for rendered output *and* committed
configuration, so anything ignoring it must enumerate. See
[[render-prunes-manifest-files-it-no-longer-owns]] for why the rendered half needs no
gitignore help in the first place.
