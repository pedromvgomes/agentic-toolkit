---
about: the curator's `agtk memory show` reads are recorded as hits, so hit rate and Cold mix consultation with curation
saw:
  - source/toolkit/internal/curator/prompt.md
  - source/toolkit/internal/cli/memory.go
  - source/toolkit/internal/memory/hits.go
targets: memory-hits-are-recorded-only-by-show
---

The note says `show` is the only recorder and that bypassing it blinds the hit rate. The
opposite error is also live: reads that are not consultation are counted.

- `source/toolkit/internal/curator/prompt.md:19` lists `agtk memory show <name>` as how the
  curator reads a note in full, with no `--no-hit`.
- `--no-hit` is defined at `source/toolkit/internal/cli/memory.go:569` and
  `grep -rn "no-hit" source/toolkit definitions docs` finds it in no definition, skill, command
  or prompt (only in tests and the flag declaration).
- `RecordHit` is unconditional otherwise (`memory.go:538-543`; `hits.go` appends one line, no
  caller identity).

So a `/memory-curate` run over N notes adds up to N hits that no question asked for, and
`stats` `notes_hit`/`hit_rate`/`cold` (`memory/stats.go`, `known[h.Note]` filter) cannot tell
them apart. `hits.jsonl` lines carry only `{note, at}` (`hits.go:19-22`), so there is no
after-the-fact way to separate them either.

Inference, not verified by running a curate: that the curator actually issues `show` per note
rather than reading `notes/` directly. Its grant and prompt name `show` as the read path.
