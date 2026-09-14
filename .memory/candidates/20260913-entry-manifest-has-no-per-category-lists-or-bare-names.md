---
about: EntryManifest has no per-category lists (skills:, instructions:, etc.) and no bare-name entry resolution at all — only stacks: (URL/./path only) and convention scanning under root:
saw:
  - source/toolkit/internal/stack/entrymanifest.go
  - source/toolkit/internal/resolver/entryscan.go
---

`EntryManifest` (`entrymanifest.go`) has no `EntryRef`-typed fields — `Stacks []ExtendsRef` is
the only entry-composition field, and `ParseExtendsRef` (called on each `Stacks[i]` in
`ParseEntryManifestBytes`) accepts only a URL or a `./path`; a bare name in `stacks:` is
rejected the same way a bare name in a stack's `extends:` is. There is no equivalent of
`Stack.EntriesFor`/bare-name-under-root resolution anywhere on `EntryManifest` — content that
isn't composed via `stacks:` is found only by `entryscan.go`'s directory scan under
`EffectiveRoot()`, never by naming an individual catalog entry in the manifest itself. This is a
deliberate simplification (ADR 0016): the entry manifest trades the ability to name one catalog
entry directly for a flatter, convention-only surface.
