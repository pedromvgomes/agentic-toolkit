---
about: review-is-model-free-reviewrun-is-not still holds; pointers shifted
saw:
  - internal/review/capability.go
  - internal/reviewrun/run.go
  - internal/cli/codereview.go
  - internal/reviewrun/invoke.go
targets: review-is-model-free-reviewrun-is-not
verdict: still-true
---

Re-checked because the note was stale (nine anchored files had moved blobs).

`grep -rl agentic-driver internal/review` -> exactly `internal/review/capability.go`, as
claimed. The file's own package-doc-adjacent comment now states the invariant explicitly:
"This is the only file in the package that imports the driver, and it constructs nothing"
(`internal/review/capability.go:3-9`, not `:1-9` — the package line moved above the comment).

`internal/reviewrun/run.go:1-10` still carries "It is the only package in code-review that
invokes a model... Nothing here talks to GitHub." The driver import lives in
`internal/reviewrun/invoke.go:11` (`agentic "github.com/pedromvgomes/agentic-driver"`), one way,
as before.

`internal/cli/codereview.go`'s "deliberately model-free" comment is now at roughly lines 13-16
rather than 13-21 — shorter, same claim.

Consequence unchanged: a GitHub App credential belongs above `reviewrun`, in the CLI.
