---
name: stack-schema-is-strict-and-legacy-keys-are-intercepted
kind: gotcha
description: A stack field must exist on the struct before any manifest may use it, and four names are stolen by the v1 migration check — a fifth was reclaimed.
anchors:
  - path: source/toolkit/internal/stack/parser.go
    blob: c85fae1c0eb7
  - path: source/toolkit/internal/stack/types.go
    blob: ece8bb1e21cf
  - path: source/toolkit/internal/stack/tests/parser_test.go
    blob: 26d8cf4b9e31
confidence: verified
---

Manifests decode with `yaml.Strict()` (`source/toolkit/internal/stack/parser.go:50`), so an unknown
top-level key is a hard parse error, not an ignored field. A consumer therefore cannot
write a new setting before `stack.Stack` has the field — which is why adding `memory:`
was a schema change rather than a read.

The trap is the pre-decode check: `detectLegacyConfig` (`source/toolkit/internal/stack/parser.go:366`)
runs first and rejects `source`, `presets`, `externals` and `definitions` (the list is at
`:376`) with "is a v1 schema field" (`:370`). Those four names are unavailable for new
fields no matter what the struct says — a future top-level `definitions:` key would fail with
a migration hint that has nothing to do with the actual problem.

The list was once five. `platforms` was removed from `legacyTopLevelKeys` (now at `parser.go:376`,
four names) when it became a v2 field; the guard is a pure regex match on raw bytes
(`topLevelKeyRE`, `parser.go:400`) with no link to the struct. Since then `platforms:` and
`memory:` moved to the entry manifest and are rejected in a stack by a separate pre-decode check,
`detectRepoOnlyFields` (see [[memory-config-is-entry-manifest-only]]). A `platforms:` key in a
stack is therefore an `ErrRepoOnlyField` error, not a legacy-migration one. The earlier
`platforms_test.go` pin no longer exists; nothing pins the four remaining legacy names.

Struct tags are load-bearing beyond decoding: `agtkdoc` feeds the generated schema docs.
See [[generated-schema-docs-have-no-ci-guard]].
