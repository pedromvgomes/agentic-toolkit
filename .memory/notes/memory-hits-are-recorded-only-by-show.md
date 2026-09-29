---
name: memory-hits-are-recorded-only-by-show
kind: gotcha
description: "`agtk memory show` is the only thing that records a note hit, so any retrieval path that bypasses it blinds the hit rate and the cold-note prune list."
anchors:
  - path: source/toolkit/internal/cli/memory.go
    blob: 406ceab0eb4d
  - path: source/toolkit/internal/memory/hits.go
    blob: 30d823466c1e
  - path: source/toolkit/internal/memory/index.go
    blob: 372f7c004865
confidence: verified
---

`RecordHit` (`source/toolkit/internal/memory/hits.go:26`) has exactly one non-test caller:
`source/toolkit/internal/cli/memory.go:540`, inside `show` unless `--no-hit` is passed. It
appends to a local, gitignored log. `stats` hit_rate and its `cold` list derive from that log, so
an explorer or tool that reads `notes/*.md` directly, or through an index-only route, records
nothing and makes healthy notes look cold.

Retrieval is deliberately a read-of-the-whole-`INDEX.md` routing table then `show` per selected
note. Nothing caps the index: `RenderIndex` (`index.go`) emits every note, and lint requires only
a non-empty single-line description. No ADR weighs an MCP/RAG/embedding alternative; ADR 0002
(no model calls in agtk) is the nearest argument against an embedding step.
