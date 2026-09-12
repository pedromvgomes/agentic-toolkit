---
about: a review reads CONTEXT.md (and the other default convention docs) as literal root-level text, at the base ref, and never follows anything it imports
saw:
  - source/toolkit/internal/reviewrun/prompt.go
  - docs/adr/0007-untrusted-heads-are-closed-structurally.md
---

Checked whether CONTEXT.md's role as a panel convention document is recorded anywhere and
whether there is a known gotcha about adding rules to it, since no memory note covers it.

`reviewrun.DefaultConventionDocs` (`source/toolkit/internal/reviewrun/prompt.go:91-105`) lists
`CONTEXT.md` among the documents a panel is held against when the manifest names none. The
comment directly above it states the constraint: "Read at the repo root only, and never
followed: a document that imports another is read as the text it is, because following imports
means resolving paths written on the branch under review." `runnerBody` (`prompt.go:74-88`)
reads bodies (and, per ADR 0007 and the comment at `manifest.go`, the manifest and any
repo-local convention doc) from the base ref, never the tree under review, so that a branch
cannot rewrite the rules that judge it.

Two consequences for adding vocabulary to CONTEXT.md:

- The file is read as its own literal text by the reviewer. An `@import`-style reference to
  another file (the pattern `CLAUDE.md` uses toward `RTK.md`/knowledge files, per
  `agents-md-lookup-prefers-the-stack-dir` and similar) would not be resolved for review
  purposes — anything a reviewer needs to enforce a naming rule must live in CONTEXT.md's own
  text, not in a file it points to.
- Whatever is added lands in the panel's view only after being committed to the base ref a
  review runs against; editing CONTEXT.md on the same branch under review does not change what
  that review reads for it.

No existing memory note covers this; there is nothing to mark stale.
