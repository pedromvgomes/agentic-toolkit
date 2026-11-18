---
about: "A release workflow cannot be triggered by a tag it pushes with GITHUB_TOKEN, so auto-release.yml calls release.yml as a reusable workflow on every push to main, and a tag pushed by hand releases nothing"
saw:
  - .github/workflows/auto-release.yml
  - .github/workflows/release.yml
  - docs/adr/0030-a-release-is-the-merge-of-its-notes.md
---

`release.yml` has no `push: tags` trigger and is a `workflow_call` (plus a `workflow_dispatch` that
finishes an existing tag). A tag pushed with `GITHUB_TOKEN` starts no workflow, so the `tag` job
could never start a tag-triggered build; `auto-release.yml` therefore calls `release.yml` directly
after `plan` finds a version whose `docs/releases/vX.Y.Z.md` exists and whose tag does not.

The permission seam is in `release.yml`: `verify` holds a read-only token and is the only job that
runs the repository's checks, `tag` pushes one annotated tag and runs no repository code, and
`release` builds at the commit `verify` resolved. In `finish` mode `verify` requires the tag's
commit to be on `origin/main`; the tagger email it also reads is a sanity check, not provenance,
because anyone who can push a tag can set it. The concurrency group of `release.yml` is keyed by the
version, so a sweep and a dispatched finish never publish one tag at once.

Established by reading the two workflows and ADR 0030, and by `actionlint` on both. The first
release cut this way had not run when this was written.
