---
about: definitions/SCHEMA.md describes the CLAUDE.md/AGENTS.md managed region as `agtk:start`/`agtk:end`, but the Claude adapter's actual markers are HTML comments with different text
saw:
  - source/toolkit/internal/schemadoc/schemadoc.go
  - source/toolkit/internal/adapters/claude/instructions.go
---

`definitions/SCHEMA.md` is generated (`make generate`) from struct doc comments in
`internal/schemadoc`, not hand-written. Its instruction-category intro string is a literal in
`internal/schemadoc/schemadoc.go:78`: `"...inside an `agtk:start`/`agtk:end` managed region..."`.

The Claude adapter's real markers are `<!-- BEGIN AGTK MANAGED -->` / `<!-- END AGTK MANAGED
-->` (`internal/adapters/claude/instructions.go:15-17`). Nothing in the adapters emits
`agtk:start`/`agtk:end` — that string exists only in the generated-docs literal.

Fixing this means editing the `Intro` literal in `schemadoc.go:78`, then running `make
generate` to regenerate `definitions/SCHEMA.md` — editing `SCHEMA.md` directly would be
overwritten by the next `make generate`.
