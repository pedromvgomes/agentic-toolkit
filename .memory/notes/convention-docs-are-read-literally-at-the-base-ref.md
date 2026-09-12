---
name: convention-docs-are-read-literally-at-the-base-ref
kind: gotcha
description: A review reads convention docs (CONTEXT.md, CLAUDE.md, …) as literal root-level text at the base ref, so an imported file is not resolved and a rule added on the branch under review does not judge it.
anchors:
  - path: source/toolkit/internal/reviewrun/prompt.go
    blob: 51048d1eac11
  - path: docs/adr/0007-untrusted-heads-are-closed-structurally.md
    blob: 4872a6344fcf
confidence: verified
---

`reviewrun.DefaultConventionDocs` (`source/toolkit/internal/reviewrun/prompt.go:97-105`) is
the list a panel is held against when the manifest names none: `CLAUDE.md`, `AGENTS.md`,
`.claude/CLAUDE.md`, `CONTEXT.md`, `CONTRIBUTING.md`, `docs/ARCHITECTURE.md`,
`docs/CODE_STANDARDS.md`. The constraint is in the doc comment above it (`:93-96`): "Read at
the repo root only, and never followed: a document that imports another is read as the text
it is, because following imports means resolving paths written on the branch under review."

Two consequences when adding a rule a reviewer is meant to enforce:

- **An import is not resolved.** `readConventions` (`prompt.go:122-142`) does one
  `git show <baseRef>:<name>` per name (`prompt.go:126-127`) and stores the raw bytes
  (`prompt.go:139`) — there is no second pass over the body. An `@import` line in `CLAUDE.md`
  reaches the reviewer as the literal `@`-line and nothing more, so the rule's text must live
  in the convention document itself.
- **The branch's own copy is not read.** Everything is read at the base ref, never the tree
  under review — the same closure `LoadAtRef` makes for the manifest, and `runnerBody`
  (`prompt.go:68-89`) for prompt bodies, per
  `docs/adr/0007-untrusted-heads-are-closed-structurally.md`. A change that adds a rule is
  therefore not judged against it; the rule binds from the commit after it merges. The doc
  comment at `prompt.go:119-121` records this consequence explicitly.

A missing default is silent; a document the *manifest* named and that is absent at the base
ref is collected into `missing` instead (`prompt.go:128-137`), because "the repo said its
rules live there".
