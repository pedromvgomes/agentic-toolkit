---
name: hit-fold-and-compaction-ordering
kind: invariant
description: Hit fold and compaction each write the durable copy before the destructive step, so a failure never loses a read, and each has one named window.
anchors:
  - path: source/toolkit/internal/memory/hits.go
    blob: 55841d9cd15f
  - path: source/toolkit/internal/memory/hits_compact_remove_test.go
    blob: beadd357c45a
confidence: verified
---

Both operations write the durable copy first and the destructive step second.

**Fold** (`FoldHits`, `memory/hits.go:479`): write the shard, then `os.Truncate` the log
(`hits.go:511`). A shard that cannot be written leaves the log untouched. A log that cannot be
emptied has its shard removed again, so no read is counted twice. A `show` that records a read
between the fold reading the log and truncating it loses that read (`hits.go:474-475`). Closing
the window would need a rename-based handoff, and a renamed log file would fall outside the
store's `.gitignore`, which covers only `.hits.jsonl`.

**Compaction** (`CompactHits`, `memory/hits.go:405`): read `hits.json` and every shard, write the
union to a temp file in the same directory and rename it over `hits.json` (`ReplaceHitRecord`,
`hits.go:246`), and only then remove the folded shards (`hits.go:435`). A failure before the rename
leaves `hits.json` and every shard as they were.

A shard still present after the rename is counted twice by `stats` and folded in again by the next
compaction, because nothing records which shards `hits.json` already holds. The error names every
shard left in that window (`hits.go:442`); removing it by hand is the remedy.

A `hits.json` that exists but cannot be read (merge-conflict markers, for instance) makes
compaction return an error and removes nothing, so `curate` exits non-zero after verification and
clearing; replacing it would discard the reads it holds. An unreadable shard is left in place and
reported while the readable ones are folded.

`memory/hits_compact_remove_test.go` pins the order: it checks `hits.json` already holds the merged
reads at the moment each shard is removed.
