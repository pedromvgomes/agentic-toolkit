# Anchor memory notes by content hash, and derive staleness

Each anchor in a memory note records the git blob hash of the file the claim was derived
from, not the commit the note was written at. Commit anchoring fails hard under squash
merges — the branch's commits become unreachable, so `git diff <commit>..HEAD` returns
`unknown revision` rather than degrading — and squash merge is the house default. Blob
hashes are also immune to rebases, force-pushes and shallow CI clones, and they work in a
checkout with no history at all.

Staleness is recomputed from the working tree on every `agtk memory audit` and never
stored, for the same reason `INDEX.md` is generated rather than authored: stored derived
state can drift from the thing it describes. Hashes are computed in-process
(`sha1("blob <len>\0" + content)`) rather than by shelling out to git, so the check needs
no repository, matches what an agent will actually read including uncommitted edits, and
is testable from a temp directory.

## Consequences
- After a squash the previous blob may be gone, so re-verification sees "this file changed,
  here is its current content" rather than a diff. Acceptable: checking a note's pointers
  against current state is the job anyway.
- `confidence:` in a note is curator judgment only. Nothing mechanical writes it, so a note
  can be `verified` and stale at the same time — the two axes are independent.
