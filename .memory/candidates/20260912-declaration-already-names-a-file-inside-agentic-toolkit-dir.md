---
about: CONTEXT.md and code comments already use "declaration"/"configuration" in ways that collide with a proposed root-vs-.agentic-toolkit/ vocabulary
saw:
  - CONTEXT.md
  - source/toolkit/internal/review/manifest.go
  - .agentic-toolkit.yaml
---

Looked for any existing rule or vocabulary distinguishing what belongs at the repo root from
what belongs under `.agentic-toolkit/`, ahead of adding such a term to CONTEXT.md.

Two collisions with a "root = declaration, `.agentic-toolkit/` = configuration" split:

1. CONTEXT.md already calls a file *inside* `.agentic-toolkit/` a "declaration": the **Review
   manifest** glossary entry (`CONTEXT.md:459-463`) reads "`.agentic-toolkit/code-review/manifest.yaml`:
   the single declaration of Reviewers, Panels and the prompt bodies they use." So "declaration"
   is already spoken for, and it names something the directory holds, not something at the root.

2. `review.ManifestDir`'s own doc comment (`source/toolkit/internal/review/manifest.go:3-13`)
   draws a two-way split, but differently: "the manifest is configuration, not committed content
   a repo has an opinion about placing" — i.e. it calls the manifest *configuration* (fixed,
   unopinionated location under the toolkit's namespace) as opposed to *committed content* (a
   repo's own files, which it can choose to place anywhere, including somewhere an ignore rule
   would reach). That axis is committed-content-vs-configuration, not root-vs-directory, and the
   review manifest is squarely on the "configuration" side of it despite being hand-authored,
   PR-reviewable content.

Neither `docs/adr/*.md` nor CONTEXT.md's other entries (Entry manifest `:25-29`, Memory store
`:111-116`) name a root-vs-`.agentic-toolkit/` axis at all — `grep -n
"declaration\|entry manifest\|\.agentic-toolkit/" CONTEXT.md` finds only the two entries above
plus the Review manifest one. So there is no prior attempt to name this split; the two existing
uses of "declaration"/"configuration" are free-standing word choices in unrelated glossary
entries, not a considered pair. Whatever term is introduced for "root = declaration" needs to
either reconcile with or explicitly supersede the Review manifest entry's use of "declaration,"
or it will read as CONTEXT.md using the same word for two different things three lines apart.
