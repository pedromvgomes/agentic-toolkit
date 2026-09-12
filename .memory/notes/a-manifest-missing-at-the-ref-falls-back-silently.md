---
name: a-manifest-missing-at-the-ref-falls-back-silently
kind: gotcha
description: LoadAtRef reads any manifest absent from the base ref as "this repo declares none" and reviews under the embedded default roster, with nothing in the output saying so.
anchors:
  - path: source/toolkit/internal/review/builtin.go
    blob: 04e622ab978f
confidence: verified
---

`LoadAtRef` (`source/toolkit/internal/review/builtin.go:111-148`) reads the review
manifest out of git at the base ref, never off disk — that is the untrusted-head
rule (`docs/adr/0007-untrusted-heads-are-closed-structurally.md`): a branch must
not name the reviewers that judge it. Its steps:

1. `rev-parse --verify` the ref first (`:117`), so an unresolvable base is an
   error rather than a silent default.
2. `cat-file -e ref:ManifestRelPath` (`:121-123`). On failure:
3. `cat-file -e ref:LegacyManifestRelPath` (`:129-130`); if *that* hits, the old
   path is **read** (`:136`), not refused.
4. Neither present — "this repo declares no manifest at the base" (`:131-132`) —
   fall back to `DefaultManifest()` (`:133-134`) with `builtin=true` and no error.

The legacy check added when the manifest moved out of `.agents/` (see
[[committed-content-cannot-live-in-a-rendered-tree]]) narrows step 4 by exactly one
case: a manifest at the old path. Every other reason a manifest is missing from
the ref still lands in the silent fallback, and `cat-file -e` cannot tell them
apart — a manifest present in the worktree but never `git add`ed, or covered by a
`.gitignore` mistake over `.agentic-toolkit/`, reviews under the built-in roster
while the repo's own file sits on disk unread.

Step 3 is where the two loaders part. `LoadAtRef` reads the legacy path; only the
disk-based `Load` (`:157-175`) refuses it, via `legacyManifestErr` (`:94-102`,
called at `:165`). The reason is in that function's doc comment (`:91-93`): a ref
is history and `git mv` cannot reach it, so refusing at a ref would name a remedy
that does not exist for the one commit it fires on — and would make the very
change that moves the manifest unreviewable, since the base it merges into is
always the older layout.

`Load` is the sibling used outside PR review, and it would find via `os.Stat`
(`:159`) a manifest the ref-based path cannot see. So the two disagree about
whether this repo has a manifest — and the ref-based one is the path a posted
review uses.
