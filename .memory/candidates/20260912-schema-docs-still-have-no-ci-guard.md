---
about: SCHEMA.md/CONFIG-SCHEMA.md still have no CI check regenerating or diffing them, at schemagen's new location
saw:
  - .github/workflows/*.yml
  - source/toolkit/internal/schemadoc/schemadoc.go
  - source/toolkit/tools/schemagen/main.go
  - source/toolkit/internal/stack/tests/parser_test.go
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
The claim ("nothing verifies them, so they drift silently") holds at the CI level, but it is
narrower than it was for one field. `TestMemoryRootDocNamesTheRealDefault`
(`source/toolkit/internal/stack/tests/parser_test.go`) asserts that the `agtkdoc` tag on
`stack.MemoryConfig.Root` — the string CONFIG-SCHEMA.md's `memory.root` row is generated from —
names `memory.DefaultRoot`. So that one value cannot drift into the published schema in a
passing build. Nothing generalises: every other `agtkdoc` string is still unguarded, and
nothing anywhere regenerates the two files and diffs the result, which is what the note is
about. A curator promoting this should keep the claim and not let the one test read as
coverage.
