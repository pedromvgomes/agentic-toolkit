---
name: stack-schema-is-strict-and-legacy-keys-are-intercepted
kind: gotcha
description: A stack field must exist on the struct before any manifest may use it, and four names are stolen by the v1 migration check — a fifth was reclaimed.
anchors:
  - path: source/toolkit/internal/stack/parser.go
    blob: d2612a7d0b55
  - path: source/toolkit/internal/stack/types.go
    blob: 67b0972a585e
confidence: verified
---

Manifests decode with `yaml.Strict()` (`source/toolkit/internal/stack/parser.go:47`), so an unknown
top-level key is a hard parse error, not an ignored field. A consumer therefore cannot
write a new setting before `stack.Stack` has the field — which is why adding `memory:`
was a schema change rather than a read.

The trap is the pre-decode check: `detectLegacyConfig` (`source/toolkit/internal/stack/parser.go:369`)
runs first and rejects `source`, `presets`, `externals` and `definitions` (the list is at
`:379`) with "is a v1 schema field" (`:372-373`). Those four names are unavailable for new
fields no matter what the struct says — a future top-level `definitions:` key would fail with
a migration hint that has nothing to do with the actual problem.

The list was five. `platforms` was removed from it when `platforms:` came back as a *v2*
field (`source/toolkit/internal/stack/types.go:64`, semantics contrasted with v1's in
`docs/MIGRATION.md:125-131`), so a stolen name can be reclaimed: the guard is a pure regex
match on raw bytes (`topLevelKeyRE`, `parser.go:385-387`) with no link to the struct, and no
test pinned `platforms` as guarded. The residual cost is that an un-migrated v1 file whose
only legacy key is `platforms:` now decodes as a v2 stack instead of getting the migration
hint — v1 also required `source:`, which is still guarded, so real v1 files are still caught.

Struct tags are load-bearing beyond decoding: `agtkdoc` feeds the generated schema docs.
See [[generated-schema-docs-have-no-ci-guard]].
