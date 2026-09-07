---
about: tools/schemagen discovers no types automatically; every documented struct is named by an explicit reflect.TypeOf call in main.go, so a new struct is invisible to the generator until someone adds that call
saw:
  - tools/schemagen/main.go
targets: generated-schema-docs-have-no-ci-guard
verdict: still-true
---

Re-checked while answering how schemagen discovers types and what tag vocabulary it
understands, for a subsystem (`agtk code-review`) planning to reuse the same `agtkdoc`
convention. The existing note's claim (nothing regenerates or diffs the generated docs) still
holds — `.github/workflows/*.yml` has no schemagen/generate step — but it does not cover the
discovery mechanism, which is the part worth recording.

There is no package walk or type registry: every documented top-level struct is an explicit
`reflect.TypeOf(...)` call written by hand in `tools/schemagen/main.go`, e.g.
`docForType(reflect.TypeOf(stk.Stack{}))` (`main.go:238`), `...lock.Lockfile{}` (`:292`), and
one `categoryDoc` entry per definitions category with a `Sample defs.Definition` field
(`main.go:35-51` for the struct shape, `categories` slice starting `:52`). Nested sub-structs
(extension blocks, `OAuthConfig`, `HookHandler`, …) are the one part that *is* discovered
automatically, via `collectSubStructs`/`walkSubStructs` (`main.go:412-430`ish), which recurses
into any field whose type is a pointer-to-struct — but only starting from a type that was
itself named in one of the explicit `reflect.TypeOf` call sites above.

Failure mode: a wholly new top-level manifest struct (e.g. a `CodeReviewConfig` analogous to
`stk.MemoryConfig`) produces zero docs and zero errors until someone adds its own
`reflect.TypeOf` line — `go generate ./...` succeeds silently either way.

Tag vocabulary understood by `docForType` (`main.go:377-408`):
- `yaml:"name,omitempty"` — the field's doc name and whether it is optional; a bare `yaml:"-"`
  field is skipped entirely; `,inline` makes the field vanish from field docs (returns `""`
  as name, `parseYAMLTag` at `main.go:516-532`).
- Anonymous embedded structs are flattened into the parent's field list rather than
  documented as a nested type (`main.go:384-387`).
- `agtkdoc:"..."` is the description. A `"required;"` prefix marks a field required
  regardless of `omitempty` (`parseAgtkdoc`, `main.go:534-542`); absent both `omitempty` and
  an `agtkdoc` tag, the field defaults to required (`main.go:398-400`). So a field with
  neither tag reads as required by default — an oversight (forgetting `agtkdoc` on an
  optional-but-untagged field) silently documents it as mandatory rather than failing to
  build.
