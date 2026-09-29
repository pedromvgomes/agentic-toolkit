---
name: memory-hits-are-recorded-only-by-show
kind: gotcha
description: "`agtk memory show` is the only thing that records a note hit unless --no-hit is passed, so a retrieval path that bypasses it blinds the hit rate; the curator's no-hit is prompt-only."
anchors:
  - path: source/toolkit/internal/cli/memory.go
    blob: ac2b4d13579f
  - path: source/toolkit/internal/memory/hits.go
    blob: 30d823466c1e
  - path: source/toolkit/internal/memory/index.go
    blob: 44e4ee0ed4cd
  - path: source/toolkit/internal/curator/prompt.md
    blob: fe8576dcadf8
  - path: definitions/agents/memory-explorer/AGENT.md
    blob: 02e85111f1fc
confidence: verified
---

`RecordHit` (`memory/hits.go:26`) has one non-test caller, `cli/memory.go:546`, inside `show` unless
`--no-hit` (declared `cli/memory.go:575`). It appends to a local, gitignored log; `stats` hit_rate
and `cold` derive from it, so a reader of `notes/*.md` directly, or via search (records nothing),
makes healthy notes look cold. `hits.jsonl` lines carry only `{note, at}` (`hits.go:19-22`), so a
hit does not say who read.

- The curator reads with `--no-hit` (`prompt.md:21-22`). Prompt only: its grant is
  `Bash(agtk memory show *)` (`curator.go:323`), which admits the flag but cannot require it.
  `curator/tests/no_hit_test.go:13` checks the prompt text, not behaviour.
- The explorer's reads record hits on purpose: `AGENT.md` Step 3 (lines 96-113) opens notes through
  `show`, because the hit rate measures consultation.
- The `INDEX.md` header names both forms (`memory/index.go:37`); lint fails on header drift from
  `RenderIndex`, so editing that line needs `agtk memory index`.
