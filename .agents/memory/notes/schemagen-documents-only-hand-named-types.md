---
name: schemagen-documents-only-hand-named-types
kind: gotcha
description: schemagen discovers no top-level types; each one is a hand-written reflect.TypeOf call, so a new manifest struct documents as nothing and errors as nothing.
anchors:
  - path: source/toolkit/tools/schemagen/main.go
    blob: effbadf9177a
confidence: verified
---

There is no package walk and no registry. Every documented top-level struct is an explicit
`reflect.TypeOf(...)` call someone wrote in `source/toolkit/tools/schemagen/main.go` —
`stk.Stack{}` at `:241`, `stk.MemoryConfig{}` at `:248`, `sourceref.Source{}` at `:271`,
`rev.Manifest{}` at `:297` (then `rev.Runner{}` `:304`, `rev.Panel{}` `:309`,
`rev.Defaults{}` `:316`, `rev.Escalation{}` `:323`), `lock.Lockfile{}` at `:366` — plus one
`categoryDoc` entry per definitions category (`categories` at `:48`, shape at `:37`).

Nested sub-structs *are* found automatically: `collectSubStructs`/`walkSubStructs`
(`main.go:490`, `:498`) recurse into fields whose type is a pointer-to-struct — but only from
a type already named in one of the call sites above.

Failure mode: a wholly new top-level manifest struct produces zero docs and zero errors.
`go generate ./...` succeeds either way, and [[generated-schema-docs-have-no-ci-guard]] means
nothing downstream notices.

Second trap, in `docForType` (`main.go:452`): a field with neither `omitempty` nor an
`agtkdoc` tag is documented as **required** — `if !omitempty && desc == "" { required = true }`
at `main.go:474-475`. Forgetting the tag on an optional field silently publishes it as
mandatory. The rest of the tag vocabulary: `yaml:"-"` skips the field, `,inline` makes it
vanish (`parseYAMLTag`, `:590`), an anonymous embedded struct is flattened into the parent's
field list (`:459-462`), and a `required;` prefix on `agtkdoc` forces required regardless of
`omitempty` (`parseAgtkdoc`, `:611`).
