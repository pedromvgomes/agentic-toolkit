# Memory retrieval is a file scan behind a ranker

An explorer that wants the notes bearing on a question needs a way to find them without opening
all of them. The **Index** lists every note with its description and anchor paths, so an explorer
could read that table and pick. It pays for the whole table on every delegation, whatever the
question, and it leaves the choosing to a model reading rows. The store's notes are anchored to
files and named for what they are about, so the question an explorer has, "which notes bear on
these files and these words", has a mechanical answer.

## Decision

**`agtk memory search` ranks notes with no model call.** `agtk memory search [--files a,b]
[words…] [--limit N] [--json]` returns the best-matching notes, each with its name, kind,
confidence, score, description, the anchor paths that cover a named file, and the `agtk memory
show` command that opens it. A query naming neither a file nor a word is a usage error.

**A file match outranks every word match.** A note with an anchor covering a named file ranks above
any note that matches on words alone, and a note covering more of the named files ranks above one
covering fewer. A concrete anchor covers the path it names; a glob anchor covers what `path.Match`
says it matches, which is one directory level, and a `**` anchor covers nothing
(`docs/adr/0005-glob-anchors-mark-quantified-claims.md`). Words are lower-cased alphanumeric
tokens, scored over a note's name (3), description (2) and body (1), with each term's count capped
at 3 per field, so a long note that repeats a word does not outrank a short note that is about it.
Results are ordered by score descending, then name ascending, so the same tree gives the same list
in the same order.

**The ranker reads notes through a `Corpus`.** `Corpus` is a small interface: every note, plus one
error per note that could not be read, and whether a note is stale. Its one implementation scans
`notes/` on every call. Nothing is cached between searches, so a result always reflects the notes
on disk, and an unreadable note is reported on stderr rather than dropped.

**Search reads and nothing else.** It records no **Hit**, writes nothing under the store, and does
not demote a stale note. `stale` is a label, computed by the same audit `agtk memory show` reports,
and it never moves a note's rank: staleness says the anchored files moved, not that the claim is
wrong (`docs/adr/0001-anchor-notes-by-content-hash.md`). A Hit is recorded only by `show`, so the
hit rate keeps measuring reads of a note's content and not lookups that routed past it.

**The explorer searches, and opens notes only through `show`.** The memory-explorer routes with
`agtk memory search` and opens each note it selects with `agtk memory show`. It does not read
`INDEX.md` to route. The index stays committed, generated and lint-checked, the curator still works
from it, and the explorer's read grant on it stays. No fallback exists for an `agtk` older than
`search`; the explorer's routing requires the command.

## Considered options

**SQLite, or any built store, as the store.** Rejected: notes are committed, reviewed as diffs in
PRs, and have exactly one writer, the curator
(`docs/adr/0003-the-curator-is-the-only-author-of-notes.md`). A database file is neither reviewable
text nor something one writer can be held to.

**A session-start `build-index`, or another derived index.** Rejected: an index built at session
start is wrong the moment a note or an anchored file changes mid-session, and it is a second source
of truth with its own parity to maintain against the notes. At the size a store has, it buys nothing
a scan does not.

**An MCP server.** Rejected: a long-running process to answer what a deterministic CLI call answers
from the files on disk.

**Embeddings or RAG.** Rejected for `agtk` itself: computing an embedding is a model call, which
`docs/adr/0002-no-model-calls-in-agtk.md` keeps out of `agtk`'s deterministic path. An embedding
index would be an opt-in tool outside the binary, not something `agtk memory` grows.

## Measured

`BenchmarkSearch` in `source/toolkit/internal/memory/tests/search_test.go` runs one search over a
synthetic 2,000-note store, file scan and parse included, and reports about 180 ms per search on the
machine that implemented it, about 78% of it YAML decoding in `LoadNotes`. A real store holds 54 to
120 notes. Cost grows with note count, so a search there is a small fraction of that figure, in the
low tens of milliseconds at most. That is an extrapolation from the 2,000-note figure, not a
measurement of a real store.

## Revisit trigger

A search over a real store that takes more than about 100 ms, measured with `BenchmarkSearch` scaled
to that store's note count, reopens this decision.

## Consequences

- ADR 0002 holds. Search is deterministic and calls no model, so it is safe on the path of a hook and
  in CI.
- ADR 0003 holds. Search writes nothing under `notes/`, nor anywhere else in the store.
- The `Corpus` interface is the seam. A derived index, or another ranker's input, can sit behind it
  and leave the query, the ranking contract and the output unchanged; that is a change to its
  implementation, and adding one is a decision to make against the trigger above.
- The explorer's routing costs a handful of ranked results per delegation rather than the whole
  index, and a delegation that finds nothing anchored on its files or words gets
  `no matching notes` rather than a table to read.
- Every search parses every note. The cost is linear in the store, which is what the trigger above
  bounds.
- A search that ranks a stale note high says nothing about whether it is still true. The explorer
  still re-checks what `show` reports as `stale: yes`.
- The wording of the `index_bytes` line `stats` prints is not settled here.
