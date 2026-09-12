---
about: schemagen's logic moved out of tools/schemagen/main.go into internal/schemadoc/schemadoc.go; the hand-named-types claim still holds at the new location
saw:
  - source/toolkit/tools/schemagen/main.go
  - source/toolkit/internal/schemadoc/schemadoc.go
targets: schemagen-documents-only-hand-named-types
verdict: still-true
---

Re-checked because the note was stale on its one anchor.

`source/toolkit/tools/schemagen/main.go` is now a 19-line wrapper (`func main` calling
`schemadoc.Generate()`); its own doc comment says why: "this package holds nothing but func
main, which no test can call, so that nothing untestable sits next to code that is." All the
logic the note describes now lives in `source/toolkit/internal/schemadoc/schemadoc.go`.

The claim itself is unchanged: every top-level type is still a hand-written
`reflect.TypeOf(...)` call — `stk.Stack{}` (`schemadoc.go:256`), `stk.MemoryConfig{}` (`:263`),
`sourceref.Source{}` (`:286`), `rev.Manifest{}`/`Runner{}`/`Panel{}`/`Defaults{}`/`Escalation{}`
(`:312,319,324,331,338`), `lock.Lockfile{}`/`ResolvedSource{}` (`:381,386`), plus
`defs.Common{}` (`:217`, `:572`) — with `collectSubStructs`/`walkSubStructs` (`:505`,`:513`)
still recursing into pointer-to-struct fields only from a type already named above. The
required-by-omission trap is also unchanged: `docForType` (`:467`) still does
`if !omitempty && desc == "" { required = true }` at `:489`, and `parseAgtkdoc`'s
`required;` prefix override is still there (`:623-628`).

So a note anchored at `source/toolkit/tools/schemagen/main.go` is anchored at the wrong file
now; the pointer that matters is `source/toolkit/internal/schemadoc/schemadoc.go`.
