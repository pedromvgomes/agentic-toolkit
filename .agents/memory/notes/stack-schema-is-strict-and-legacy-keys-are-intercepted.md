---
name: stack-schema-is-strict-and-legacy-keys-are-intercepted
kind: gotcha
description: A stack field must exist on the struct before any manifest may use it, and five names are stolen by the v1 migration check.
anchors:
  - path: internal/stack/parser.go
    blob: cdbb1d5c5dec
  - path: internal/stack/types.go
    blob: fe5cd947f8d3
confidence: verified
---

Manifests decode with `yaml.Strict()` (`internal/stack/parser.go:47`), so an unknown
top-level key is a hard parse error, not an ignored field. A consumer therefore cannot
write a new setting before `stack.Stack` has the field — which is why adding `memory:`
was a schema change rather than a read.

The trap is the pre-decode check: `detectLegacyConfig` (`internal/stack/parser.go:362`)
runs first and rejects `source`, `presets`, `externals`, `definitions` and `platforms`
(the list is at `:372`) with "is a v1 schema field" (`:366`). Those five names are
unavailable for new fields no
matter what the struct says — a future top-level `definitions:` key would fail with a
migration hint that has nothing to do with the actual problem.

Struct tags are load-bearing beyond decoding: `agtkdoc` feeds the generated schema docs.
See [[generated-schema-docs-have-no-ci-guard]].
