---
name: generated-schema-docs-have-no-ci-guard
kind: gotcha
description: SCHEMA.md and CONFIG-SCHEMA.md are generated but no workflow regenerates or diffs them, so they drift silently.
anchors:
  - path: source/toolkit/internal/schemadoc/schemadoc.go
    blob: 7d8cd09b181a
  - path: .github/workflows/*.yml
    matches:
      - path: .github/workflows/ci-build.yml
        blob: c6d952660985
      - path: .github/workflows/ci-orchestration.yml
        blob: dc1cb6c652e4
      - path: .github/workflows/ci-preflight.yml
        blob: f01800fcff31
      - path: .github/workflows/ci-test.yml
        blob: e107a26e877a
      - path: .github/workflows/dependabot-auto-merge.yml
        blob: 42d98e95b8af
      - path: .github/workflows/gt-sync.yml
        blob: 7e4060ee41af
      - path: .github/workflows/release.yml
        blob: 3ffca47a29e3
confidence: verified
---

`definitions/SCHEMA.md` and `definitions/CONFIG-SCHEMA.md` are produced by
`source/toolkit/tools/schemagen` — a 19-line wrapper whose logic lives in
`source/toolkit/internal/schemadoc/schemadoc.go` — from the structs in
`source/toolkit/internal/{definitions,stack,lockfile,review}` via `go generate ./...`
(the directives live at `source/toolkit/internal/stack/types.go:36` and
`source/toolkit/internal/definitions/types.go:7`) — and only from the structs it was told
about by name, see [[schemagen-documents-only-hand-named-types]].

No workflow runs or checks it. `grep -rn -i "generate\|schema" .github/workflows/*.yml`
returns two hits, both unrelated comments (that `tools/schemagen` holds nothing testable,
and goreleaser's own changelog generation). Both files had already drifted before anyone
noticed: the skill struct gained `argument_hint` and `disable_model_invocation` without a
regeneration, and the missing rows only surfaced as unrelated hunks in the memory PR
(`db0114b`).

One field, and only one, is guarded — and it is not CI coverage. The `agtkdoc` tag on
`stack.MemoryConfig.Root` (`source/toolkit/internal/stack/types.go:76`) spells the default
store root out because a struct tag cannot interpolate a constant, and
`TestMemoryRootDocNamesTheRealDefault`
(`source/toolkit/internal/stack/tests/parser_test.go:316-325`) fails if the tag stops
naming `memory.DefaultRoot`. That pins one value against one Go const. Every other
`agtkdoc` string is unguarded, and nothing anywhere regenerates the two documents and
diffs the result, which is what this note is about.

So: run `go generate ./...` in the same commit as any struct or `agtkdoc` change. A CI step
that regenerates and diffs would make this note obsolete, which is the point — the anchor
glob covers the workflows so adding one marks this stale.
