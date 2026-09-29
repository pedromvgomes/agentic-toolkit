# Hit counts are carried in committed shards, folded by `agtk` and compacted by `curate`

`agtk memory show` records each read of a **Note** as a line in the gitignored `<root>/.hits.jsonl`.
That log describes one working copy: a fresh clone reports zero reads, a cloud session's reads are
lost with its container, and the hit rate the **Memory store** is judged on is a fact about whichever
checkout happened to run `stats`. The count has to travel with the branch, like the notes it
measures, without giving the notes a second writer and without a file that two branches edit at once.

## Decision

**A fold moves the local log into a committed shard.** `agtk memory hits fold` writes one file,
`<root>/hits/<UTC-date>-<branch-slug>-<random suffix>.json`, holding for each note its read count and
its first and last read, under a `version`. Only once the shard is written is the local log emptied,
so a fold that cannot write its shard leaves the log intact; a log that cannot be emptied has its
shard removed again, so no read is counted twice. `hits fold --check` writes nothing and exits
non-zero while the log holds unfolded reads. Every fold names a file no other fold names, so two
branches that both fold never conflict on a shard. A shard or a compacted file of another `version`,
or with a count below one, is unreadable rather than guessed at.

**`agtk memory curate` compacts the shards.** After `verify()` passes, in the same post-verify step
that clears the candidates the curator processed
(`docs/adr/0022-agtk-clears-the-candidates-the-curator-processed.md`), `curate` merges every
readable shard into `<root>/hits.json`, writes it through a temporary file and a rename, and only
then removes the shards it folded. It does this on every real verified run, whatever its shape:
backlog, `--stale`, scoped or limited, because the record grows with reads and not with the backlog.
It never does it under `--dry-run` and never when verification fails. It makes no model call: which
reads were recorded is on disk, and summing them is filing. A shard that cannot be read is left in
place and reported, since removing it would discard reads nobody has counted. An unreadable
`hits.json` fails the run and leaves every shard, since replacing it would discard what it holds.

**`stats` reads the union.** The compacted file, every shard and the local log are merged. The hit
rate stays distinct notes hit over notes, and a hit counts only for a note that still exists. **Cold**
is every note with no hit across the union, and the window runs from the earliest first read to the
latest last read across it. The "this checkout only" caveat applies only while no committed record
exists; once one does, the reads still in the local log are the only ones flagged as this checkout's
alone. A record that cannot be read is named and left out of the counts rather than failing `stats`.

**`agtk` never commits.** The fold leaves the shard in the working tree. The identity a commit is
made under, the conventional-commit subject and the guard against authoring footers all live in the
git layer, in the instructions and hooks of the session that commits, and `agtk` holds none of them.
A `git commit` inside the binary would be a second, weaker path to the history. `open-pr` is the
caller that commits: it probes `agtk memory hits --help`, folds before its memory commit so the shard
lands in that commit, and checks that the store root is clean before it pushes. `/memory-curate`
carries no clean-tree check.

## Considered options

**A counter inside each note.** Rejected: it makes every read a write to `notes/`, and
`docs/adr/0003-the-curator-is-the-only-author-of-notes.md` gives that directory exactly one writer,
the curator. Telemetry would also
churn the note files whose diffs a reviewer reads to judge a claim
(`docs/adr/0001-anchor-notes-by-content-hash.md`).

**One committed counter file, updated in place.** Rejected: every branch that reads a note edits the
same file, so it conflicts on nearly every merge, and squash merges make that routine rather than
occasional.

**A committed append-only log.** Rejected: appending is a conflict whenever two branches append,
and the file grows with every read for as long as the store lives, where a count per note does not.

**Fold only at curate time.** Rejected: only the checkout that runs `curate` would count. A cloud
session reads notes, and its local log is gone when the container is, so the reads of the sessions
that do most of the reading would never be recorded.

## Accepted losses

Hit counts are telemetry, and this decision does not defend them the way it defends notes.

- Reads in a session that never opens a pull request stay in that checkout's local log. Nothing
  folds them and nothing else carries them.
- A merge conflict in `hits.json` is resolved by taking either side, and the reads only the other
  side held are lost.
- An unfolded `git checkout` that discards the working copy holding the local log takes its reads
  with it; the log is gitignored and exists nowhere else.
- A read arriving between a fold reading the log and emptying it is dropped with the rest of the
  log.
- Renaming a note discards its read history. Counts are keyed by name, and a name that no note bears
  counts toward nothing.

Each of these under-reports. A low count then reads as a store not being repaid, which prompts a
look at what to prune, not a false assurance that it is.

## Consequences

- ADR 0002 holds. Folding and compacting are deterministic and call no model; compaction rides on
  the one command that already does, at no cost to any other path.
- ADR 0003 holds. Nothing under `notes/` is written; shards and the compacted file sit beside the
  index, outside the directory the curator alone authors.
- The compacted file is written by `agtk` and not by the curator, so the curator gains no write
  path to it.
- A shard whose removal fails after the compacted file is renamed is counted twice by `stats` and
  by the next compaction, until it is removed by hand. The error names each such shard.
- Merging is the only place a shard's name matters. The date and branch make `hits/` readable at a
  glance; the random suffix is what stops two folds on one day and one branch from naming one file.
- The committed record grows by one file per fold until the next `curate` compacts it, so a store
  that is never curated accumulates shards, which `stats` still reads.
