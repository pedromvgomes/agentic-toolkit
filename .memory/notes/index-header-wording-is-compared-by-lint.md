---
name: index-header-wording-is-compared-by-lint
kind: gotcha
description: Any wording change in the generated INDEX.md header makes agtk memory lint fail on every store indexed by an older agtk.
anchors:
  - path: source/toolkit/internal/memory/index.go
    blob: 44e4ee0ed4cd
  - path: source/toolkit/internal/memory/lint.go
    blob: 69162c090427
confidence: verified
---

`Lint` calls `IndexCurrent` (`memory/lint.go:92`), which compares the committed `INDEX.md` byte for
byte with `RenderIndex` output (`bytes.Equal`, `memory/index.go:76`) and reports "index is out of
date" on a difference (`memory/lint.go:99`). The header sentence is part of the rendered text
(`memory/index.go:37`), so rewording it changes what every existing store is compared against.

After a release that rewords it, a consumer whose index was written by the older `agtk` fails
`agtk memory lint` (exit 1) until `agtk memory index` is run and committed; CI linting the store is
red meanwhile. Such a change belongs in the release notes' compatibility section.

Reproduced by restoring only the old header sentence in a scratch copy: `1 issue`, index out of date,
exit 1; the current wording passed.
