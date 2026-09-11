---
name: unlisted-definitions-are-invisible-to-render
kind: invariant
description: No resolution path enumerates definitions/, so a definition on disk that no stack lists is never opened and never reported.
anchors:
  - path: source/toolkit/internal/resolver/*.go
    matches:
      - path: source/toolkit/internal/resolver/provider.go
        blob: 2f9acfe86042
      - path: source/toolkit/internal/resolver/requires.go
        blob: dfb62483aa94
      - path: source/toolkit/internal/resolver/resolver.go
        blob: 8095f3afd96d
      - path: source/toolkit/internal/resolver/sources.go
        blob: 91aa5dda5f2e
      - path: source/toolkit/internal/resolver/types.go
        blob: 4fd32a68d9d0
  - path: source/toolkit/internal/definitions/walk.go
    blob: a7f4470eb69f
confidence: verified
---

Every definition that reaches a render is reached by a path someone *named*. Two ways in,
both addressing a file directly:

1. A stack manifest entry. `loadStack` iterates `st.EntriesFor(cat)` for each category
   (`source/toolkit/internal/resolver/resolver.go:202-203`) and populates `s.overlay` only from what that
   loop yields (`resolver.go:213-226`). Each entry becomes a computed `(bundleDir, fileName)`
   — `resolveBare` via `bareLayout` (`resolver.go:313-319`), `resolvePath` via `pathLayout`
   (`resolver.go:323-331`), `resolveURL` via the in-repo path after `.git/`
   (`resolver.go:338-341`) — and is handed to `parseFromFS` (`resolver.go:380`), which does
   `fs.Sub` + `ParseBundle`/`ParseFile` on that one location.
2. A `requires:` on an already-resolved definition. `pullOne` iterates
   `w.Definition.GetCommon().Requires` (`source/toolkit/internal/resolver/requires.go:99`) and resolves each
   through the same `resolveBare` (`requires.go:116-119`) — a name, not a scan.

There is no third way. The closure is seeded by manifest entries and grown only by
requirements of things already in it, so it can never contain a file nobody referenced.

The consequence: `definitions/skills/foo/SKILL.md` can exist, parse cleanly, and be listed by
nothing. `agtk render` will not open it and will not mention it. There is no "found an
unreferenced definition" diagnostic, because there is no enumeration to produce one — the
resolver's diagnostics are all about entries it *did* try to resolve (`DiagOverride` at
`resolver.go:216`, `DiagUnresolvedRequirement` at `requires.go:105`). Adding a definition file
is not adding a definition; listing it in a stack is.

Do not read `source/toolkit/internal/definitions/walk.go` as evidence against this — and do not read it as
dead code either. `WalkCatalog` (`walk.go:31`) and `isEntryPoint` really do enumerate
`definitions/`, and they have callers:
`source/toolkit/internal/definitions/tests/catalog_refs_test.go:19,47,130` and
`source/toolkit/internal/definitions/tests/catalog_test.go:23`. `catalog_refs_test.go` walks the *real*
`definitions/` tree and asserts every `requires:` in the catalog resolves to a definition the
catalog holds. That is a CI guard rail over the whole tree — it is just not on the render
path. Both walking call sites are under `source/toolkit/internal/definitions/tests/`; no non-test file calls
`WalkCatalog`.

`source/toolkit/internal/resolver/*.go` is a glob because the claim quantifies — *no* resolution path
enumerates the catalog. What falsifies it is a discovery pass that does not exist yet, most
likely a new file in that package, which per-file anchors could never notice (docs/adr/0005).
`walk.go` is anchored by name because wiring `WalkCatalog` into production is the other way
this stops being true.
