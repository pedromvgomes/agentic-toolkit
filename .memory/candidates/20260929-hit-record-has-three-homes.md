---
about: "The hit record lives in three places at once, and stats sums all three"
saw:
  - source/toolkit/internal/memory/hits.go
  - source/toolkit/internal/memory/stats.go
  - source/toolkit/internal/curator/curator.go
  - source/toolkit/internal/cli/memory.go
---

A note that says the hit log is local to one checkout describes only one of the three places a
read can be, and the claim is false once a fold or a compaction has run.

- Local log: `<root>/.hits.jsonl`, gitignored, appended by `show` alone (`RecordHit`,
  `memory/hits.go`).
- Shards: `<root>/hits/<UTC-date>-<branch-slug>-<suffix>.json`, committed. `agtk memory hits fold`
  writes one per fold and then empties the log (`FoldHits`, `memory/hits.go:479`).
- Compacted file: `<root>/hits.json`, committed. `curate` folds every readable shard into it
  after `verify()` passes, on every real verified run and never on `--dry-run`
  (`curator/curator.go:639`, `CompactHits`, `memory/hits.go:405`).

`stats` reads all three: `SharedHits` (`memory/hits.go:334`) returns the compacted file plus the
shards, and `Stats` adds the log (`memory/stats.go:104-116`), so `LocalHits`, `Shards` and
`Compacted` say where the reads came from. Hit rate is still distinct notes hit over notes, and a
hit counts only for a note that still exists.

The hit-rate line says "this checkout only" only while no shard or compacted file exists. Once
either is read, that line reports the union, and reads still in the log are shown on a separate
`unfolded:` line that keeps the wording for them alone (`cli/memory.go:780-795`). `notes:`, `candidates:` and `stale:` lines are parsed by prefix by the
session-start hook and must not change.

Established by reading `memory/hits.go`, `memory/stats.go` and `curator/curator.go`, and by
`agtk memory stats` on a store with an unfolded log.
