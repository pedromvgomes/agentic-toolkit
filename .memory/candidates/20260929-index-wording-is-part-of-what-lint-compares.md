---
about: "Any wording change in the generated INDEX.md header makes agtk memory lint fail on every store indexed by an older agtk"
saw:
  - source/toolkit/internal/memory/index.go
  - source/toolkit/internal/memory/lint.go
---

`Lint` calls `IndexCurrent` (`memory/lint.go:92`), which compares the committed `INDEX.md` byte for
byte with what `RenderIndex` produces from the notes (the `bytes.Equal` at `memory/index.go:76`),
and reports `index is out of date — run `agtk memory index`` when they differ
(`memory/lint.go:100`). The header
sentence is part of the rendered text (`memory/index.go:37`), so editing that sentence changes what
every existing store's index is compared against, not only what a newly indexed store prints.

The consequence is an upgrade step, not a bug in the store: after a release that rewords the header,
a consumer whose committed `INDEX.md` was written by the older `agtk` fails `agtk memory lint` (exit
1) until `agtk memory index` is run and the result committed. A CI job that lints the store is red in
the meantime. A change to the header therefore belongs in the release notes' compatibility section.

Established by taking this repository's current index, replacing only the header sentence with its
earlier wording (`Read one with \`agtk memory show <name>\`.`) in a scratch copy of the store, and
running `agtk memory lint`: `notes: 61 checked, 1 issue`, `.memory/INDEX.md: index is out of date`,
exit 1. The control with the current wording printed `notes: 61 ok`, `index: current`, exit 0.
