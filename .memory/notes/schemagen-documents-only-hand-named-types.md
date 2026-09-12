---
name: schemagen-documents-only-hand-named-types
kind: gotcha
description: schemagen discovers no top-level types; each one is a hand-written reflect.TypeOf call, so a new manifest struct documents as nothing and errors as nothing.
anchors:
  - path: source/toolkit/internal/schemadoc/schemadoc.go
    blob: 7d8cd09b181a
confidence: verified
---

There is no package walk and no registry. Every documented top-level struct is an explicit
`reflect.TypeOf(...)` call someone wrote in
`source/toolkit/internal/schemadoc/schemadoc.go` — `defs.Common{}` at `:217` (and `:572`),
`stk.Stack{}` at `:256`, `stk.MemoryConfig{}` at `:263`, `sourceref.Source{}` at `:286`,
`rev.Manifest{}` at `:312` (then `rev.Runner{}` `:319`, `rev.Panel{}` `:324`,
`rev.Defaults{}` `:331`, `rev.Escalation{}` `:338`), `lock.Lockfile{}` at `:381` and
`lock.ResolvedSource{}` at `:386` — plus one `categoryDoc` entry per definitions category
(`categories` at `:54`, shape at `:43`).

`source/toolkit/tools/schemagen/main.go` is only a 19-line wrapper around
`schemadoc.Generate()`; its doc comment says why — "this package holds nothing but func
main, which no test can call, so that nothing untestable sits next to code that is". Do not
go looking for the logic there.

Nested sub-structs *are* found automatically: `collectSubStructs`/`walkSubStructs`
(`schemadoc.go:505`, `:513`) recurse into fields whose type is a pointer-to-struct — but only
from a type already named in one of the call sites above.

Failure mode: a wholly new top-level manifest struct produces zero docs and zero errors.
`go generate ./...` succeeds either way, and [[generated-schema-docs-have-no-ci-guard]] means
nothing downstream notices.

Second trap, in `docForType` (`schemadoc.go:467`): a field with neither `omitempty` nor an
`agtkdoc` tag is documented as **required** — `if !omitempty && desc == "" { required = true }`
at `schemadoc.go:489-490`. Forgetting the tag on an optional field silently publishes it as
mandatory. The rest of the tag vocabulary: `yaml:"-"` skips the field, `,inline` makes it
vanish (`parseYAMLTag`, `:605`), an anonymous embedded struct is flattened into the parent's
field list (`:474-478`), and a `required;` prefix on `agtkdoc` forces required regardless of
`omitempty` (`parseAgtkdoc`, `:626-628`).
