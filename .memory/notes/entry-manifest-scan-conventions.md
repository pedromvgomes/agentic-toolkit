---
name: entry-manifest-scan-conventions
kind: rationale
description: "The entry manifest composes only via stacks: (URL or ./path) plus a convention scan under root; a missing category dir is silently empty while a named but missing context: file is fatal, and scanned files render last, ordered by filename."
anchors:
  - path: source/toolkit/internal/stack/entrymanifest.go
    blob: 7d07c11d4b26
  - path: source/toolkit/internal/resolver/entryscan.go
    blob: e082c4bb7853
  - path: source/toolkit/internal/adapters/claude/instructions.go
    blob: 0eacdb683976
  - path: source/toolkit/internal/adapters/codex/agentsmd.go
    blob: d2e1abfc1661
confidence: suspect
---

- No per-category lists. `EntryManifest` composes through `Stacks []ExtendsRef`
  (`stack/entrymanifest.go:28`), each parsed by `ParseExtendsRef` (`:109` loop), so a bare name
  is rejected as in a stack's `extends:`. Anything else is found only by the directory scan in
  `resolver/entryscan.go`. Attributed to ADR 0016 (not re-read here).
- Asymmetric errors, deliberate: a missing category directory is treated as empty
  (`entryscan.go:93`, `errors.Is(err, fs.ErrNotExist)`), but a read failure on a configured
  `context:` file is appended to the resolution errors (`entryscan.go:67-75`), because the
  consumer named content it does not have.
- Render order of instructions (`claude/instructions.go:105-124`, `codex/agentsmd.go:15-34`):
  the context instruction first, then stack-named ones in plan order, then locally scanned ones
  last, sorted by `EntryPath` rather than declared name, so as to reproduce the filename order
  of the scan; a file's `name:` can sort opposite to its filename.
- Local content wins collisions: it merges through the same overlay loop as stack entries under
  `StackName = ""`, appended to `StackOrder` after the entry's stacks. `IsContext` alone marks
  the `context:` instruction.

Suspect: the ADR rationale and the overlay-loop and `merge` claims were staged by an explorer and
only the pointers above were re-checked here.
