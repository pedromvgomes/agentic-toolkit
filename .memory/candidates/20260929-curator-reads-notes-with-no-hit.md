---
about: "the curator's note reads pass --no-hit by prompt alone, and the explorer's reads still record hits"
saw:
  - source/toolkit/internal/curator/prompt.md
  - source/toolkit/internal/curator/curator.go
  - source/toolkit/internal/curator/tests/no_hit_test.go
  - source/toolkit/internal/cli/memory.go
  - source/toolkit/internal/memory/index.go
  - definitions/agents/memory-explorer/AGENT.md
targets: memory-hits-are-recorded-only-by-show
verdict: still-true
---

The note's claims hold; pointers moved: `RecordHit` is defined at `memory/hits.go:26` and called
once outside tests, at `cli/memory.go:545`, inside `if !noHit`. The flag is declared at
`cli/memory.go:574`. What the note does not say is who passes it.

- The curator's reads pass `--no-hit`. `prompt.md:21-22` lists
  `agtk memory show <name> --no-hit` as the way to read a note in full and says to always pass it.
  It is prompt only: the grant is `Bash(agtk memory show *)` (`curator.go:323`), which admits the
  flag but cannot require it, so a curator that omits it records a hit.
- The prompt is checked, not the behaviour: `curator/tests/no_hit_test.go:13`
  (`TestThePromptTellsTheCuratorToReadNotesWithoutRecordingAHit`) asserts every `agtk memory show`
  line in the prompt carries `--no-hit`, and that the grant admits the longer form.
- The explorer's reads record hits on purpose: `AGENT.md` Step 3 tells it to open notes through
  `agtk memory show <name>` (lines 96-113) because the hit rate measures consultation.
- The generated `INDEX.md` header names both forms: `memory/index.go:37` renders "`agtk memory show
  <name>` opens one and records a hit; add `--no-hit` for a read that is not consulting the store."
  Lint fails on a header that differs from `RenderIndex`, so changing that line needs
  `agtk memory index`.
- `hits.jsonl` lines carry only `{note, at}` (`memory/hits.go:19-22`), so a hit does not say which
  reader made it; whether a read counts is decided only at the moment of the read, by the flag.
