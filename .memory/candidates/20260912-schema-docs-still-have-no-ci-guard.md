---
about: SCHEMA.md/CONFIG-SCHEMA.md still have no CI check regenerating or diffing them, at schemagen's new location
saw:
  - .github/workflows/*.yml
  - source/toolkit/internal/schemadoc/schemadoc.go
  - source/toolkit/tools/schemagen/main.go
targets: generated-schema-docs-have-no-ci-guard
verdict: still-true
---

Re-checked because the note was stale on all four anchors (three workflow files plus the old
`tools/schemagen/main.go` path).

`grep -rn -i "generate|schema" .github/workflows/*.yml` -> two hits, both unrelated comments
(a note that `tools/schemagen` holds nothing testable, and goreleaser's own changelog
generation). Nothing runs `go generate ./...` or diffs the generated docs.

The generator itself moved: the type-registration logic the note describes now lives in
`source/toolkit/internal/schemadoc/schemadoc.go` (see the companion candidate on
`schemagen-documents-only-hand-named-types`); `tools/schemagen/main.go` is now a thin wrapper.
The claim ("nothing verifies them, so they drift silently") is otherwise unchanged.
