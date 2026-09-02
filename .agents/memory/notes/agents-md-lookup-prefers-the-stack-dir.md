---
name: agents-md-lookup-prefers-the-stack-dir
kind: gotcha
description: CLAUDE.md seeds its @-import from the stack directory's AGENTS.md before the project root's.
anchors:
  - path: internal/adapters/claude/instructions.go
    blob: 39b42509cf63
confidence: verified
---

When project-scope render creates a CLAUDE.md that does not exist yet, it seeds it with an
`@<rel>` import of an AGENTS.md. `findAgentsImport` searches `StackDir` first and
`ProjectRoot` second (`internal/adapters/claude/instructions.go:117-119`).

In the bare-repo + worktree layout those two are different directories, so a worktree's own
AGENTS.md wins over the one at the bare root — and the import is written as a relative path
from ProjectRoot, so it can point up and out of the project. Both are deliberate, and both
are surprising if you assume the project root is searched first.

Only the first CLAUDE.md creation is affected: an existing file keeps whatever import it
has, because agtk only owns the region between its markers.
