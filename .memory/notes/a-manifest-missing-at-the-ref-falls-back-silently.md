---
name: a-manifest-missing-at-the-ref-falls-back-silently
kind: gotcha
description: LoadAtRef reads any manifest absent from the base ref as "this repo declares none" and reviews under the embedded default roster, with nothing in the output saying so.
anchors:
  - path: source/toolkit/internal/review/builtin.go
    blob: 6448dadb8f3b
confidence: verified
---

`LoadAtRef` (`source/toolkit/internal/review/builtin.go:99-127`) reads the review
manifest out of git at the base ref, never off disk — that is the untrusted-head
rule (`docs/adr/0007-untrusted-heads-are-closed-structurally.md`): a branch must
not name the reviewers that judge it. Its steps:

1. `rev-parse --verify` the ref first (`:106`), so an unresolvable base is an
   error rather than a silent default.
2. `cat-file -e ref:ManifestRelPath` (`:109-110`). On failure:
3. `cat-file -e ref:LegacyManifestRelPath` (`:111-112`); if *that* hits, refuse
   via `legacyManifestErr` (`:113`).
4. Neither present — "this repo declares no manifest at the base" (`:115-116`) —
   fall back to `DefaultManifest()` (`:117-118`) with `builtin=true` and no error.

The legacy check added when the manifest moved out of `.agents/` (see
[[committed-content-cannot-live-in-a-rendered-tree]]) narrows step 4 by exactly one
case: a manifest at the old path. Every other reason a manifest is missing from
the ref still lands in the silent fallback, and `cat-file -e` cannot tell them
apart — a manifest present in the worktree but never `git add`ed, or covered by a
`.gitignore` mistake over `.agentic-toolkit/`, reviews under the built-in roster
while the repo's own file sits on disk unread.

`Load` (`:136-151`), the disk-based sibling used outside PR review, would find
such a file via `os.Stat` (`:138`), so the two load paths disagree about whether
this repo has a manifest — and the ref-based one is the path a posted review uses.
