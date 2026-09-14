---
about: rendered instructions are ordered context: first, then stack-named (unchanged plan order), then locally scanned ones last by filename (EntryPath), not by their declared name:
saw:
  - source/toolkit/internal/adapters/claude/instructions.go
  - source/toolkit/internal/adapters/codex/agentsmd.go
---

Both `claude/instructions.go`'s and `codex/agentsmd.go`'s `orderInstructions` partition
`[]resolver.PlannedDefinition` into three groups before rendering: the one with `IsContext ==
true` (first), everything with `StackName != ""` (kept in whatever order `plan.Definitions`
already carries — not re-sorted), and everything else (`StackName == "" && !IsContext`, i.e.
locally scanned), sorted by `EntryPath` and placed last. Sorting the scanned group by
`EntryPath` rather than by the instruction's own `Name`/`description` matters because
`entryscan.go`'s directory scan (`fs.ReadDir`/`fs.WalkDir`) is itself lexicographic by filename —
sorting by `EntryPath` reproduces that scan order; sorting by declared name would not, since a
file's `name:` frontmatter can sort opposite to its filename.
