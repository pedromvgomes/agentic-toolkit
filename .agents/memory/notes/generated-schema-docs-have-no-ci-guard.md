---
name: generated-schema-docs-have-no-ci-guard
kind: gotcha
description: SCHEMA.md and CONFIG-SCHEMA.md are generated but nothing verifies them, so they drift silently.
anchors:
  - path: tools/schemagen/main.go
    blob: 2f9ab4c334c5
  - path: .github/workflows/*.yml
    matches:
      - path: .github/workflows/ci-build.yml
        blob: 6c4ccc31ed9b
      - path: .github/workflows/ci-orchestration.yml
        blob: aac1a99fde15
      - path: .github/workflows/ci-preflight.yml
        blob: f01800fcff31
      - path: .github/workflows/ci-test.yml
        blob: 45ddaee79893
      - path: .github/workflows/dependabot-auto-merge.yml
        blob: 42d98e95b8af
      - path: .github/workflows/gt-sync.yml
        blob: 7e4060ee41af
      - path: .github/workflows/release.yml
        blob: 2acee3607652
confidence: verified
---

`definitions/SCHEMA.md` and `definitions/CONFIG-SCHEMA.md` are produced by
`tools/schemagen` from the structs in `internal/{definitions,stack,lockfile}` via
`go generate ./...` (the directive lives at `internal/stack/types.go:35`).

No workflow runs or checks it. Both files had already drifted before anyone noticed: the
skill struct gained `argument_hint` and `disable_model_invocation` without a regeneration,
and the missing rows only surfaced as unrelated hunks in the memory PR (`db0114b`).

So: run `go generate ./...` in the same commit as any struct or `agtkdoc` change. A CI step
that regenerates and diffs would make this note obsolete, which is the point — the anchor
glob covers the workflows so adding one marks this stale.
