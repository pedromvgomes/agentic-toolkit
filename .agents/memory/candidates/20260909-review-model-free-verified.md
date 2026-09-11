---
about: review-is-model-free-reviewrun-is-not still holds; pointers shifted
saw:
  - source/toolkit/internal/review/capability.go
  - source/toolkit/internal/reviewrun/run.go
  - source/toolkit/internal/cli/codereview.go
  - source/toolkit/internal/reviewrun/invoke.go
targets: review-is-model-free-reviewrun-is-not
verdict: still-true
---

Re-checked because the note was stale (nine anchored files had moved blobs).

`grep -rl agentic-driver source/toolkit/internal/review` -> exactly `source/toolkit/internal/review/capability.go`, as
claimed. The file's own package-doc-adjacent comment now states the invariant explicitly:
"This is the only file in the package that imports the driver, and it constructs nothing"
(`source/toolkit/internal/review/capability.go:3-9`, not `:1-9` — the package line moved above the comment).

`source/toolkit/internal/reviewrun/run.go:1-10` still carries "It is the only package in code-review that
invokes a model... Nothing here talks to GitHub." The driver import lives in
`source/toolkit/internal/reviewrun/invoke.go:11` (`agentic "github.com/pedromvgomes/agentic-driver"`), one way,
as before.

`source/toolkit/internal/cli/codereview.go`'s "deliberately model-free" comment is now at roughly lines 13-16
rather than 13-21 — shorter, same claim.

Consequence unchanged: a GitHub App credential belongs above `reviewrun`, in the CLI.
