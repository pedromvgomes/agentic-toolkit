---
name: bash-writes-are-refused-inside-the-working-directory
kind: gotcha
description: A provider-side guard, separate from the tool allowlist, was once seen refusing Bash file creation inside the working directory; the curator's backlog clearing no longer depends on its rm grant.
anchors:
  - path: source/toolkit/internal/curator/curator.go
    blob: cb07c4d5ea3f
  - path: source/toolkit/internal/curator/verify.go
    blob: deee1e17d6ff
confidence: suspect
---

`allowedTools` scopes deletion to the store: `Bash(rm <candidatesDir>/*)` (`curator.go:362-364`)
and its notes twin (`:370-372`), asserted by `curator/tests/curator_test.go:116-118`. The dirs come
from `store.CandidatesPath()` (`memory/store.go:93`) made absolute (`store.go:31-38`), so the grant
is an absolute pattern; unverified inference that a relative `rm`, `rm -f` or `git rm` may not match.

Unreproduced report (suspect): the provider applies its own working-directory guard to Bash file
operations. A `touch` inside `<worktree>/.agents/memory/candidates/` was refused ("may only create
or modify files in the allowed working directories") though the path was inside; `touch docs/...`
refused identically, no symlinks were involved, and the Write tool worked in the same session. An
earlier report of `rm` being refused did not reproduce. Take "a `Bash(...)` grant is necessary, not
sufficient" as the transferable shape.

What is established: a refused candidate `rm` does not leave a resolved candidate staged. `Run`
calls `clearResolved` after `verify` passes (`curator.go:620-627`), deleting with `os.Remove` from
agtk's process (`verify.go:268`), only for ids reported resolved and staged at start
(`verify.go:255-257`). An uncleared `candidates/` after a passing run means the report did not name
it, or verification failed or it was a dry run. The notes twin has no backstop: a refused `rm` on a
retracted note fails verification (`verify.go:168-173`). See [[curate-verification-is-bidirectional]].
