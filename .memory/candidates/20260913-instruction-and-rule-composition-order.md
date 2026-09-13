---
about: instructions a stack names still render alphabetically by declared name, unchanged; local.instructions is the one path where authored (filename) order is load-bearing; rules never inline into AGENTS.md/CLAUDE.md — they render as separate files, with codex synthesizing a literal "## Rules" index heading that Claude's adapter never emits, including for locally-scanned rules
saw:
  - source/toolkit/internal/resolver/resolver.go
  - source/toolkit/internal/resolver/local.go
  - source/toolkit/internal/adapters/claude/instructions.go
  - source/toolkit/internal/adapters/claude/files.go
  - source/toolkit/internal/adapters/codex/agentsmd.go
  - source/toolkit/internal/adapters/codex/files.go
---

`local: {instructions: dir, ...}` directory scanning, with filename-order composition, has
landed. Two things this reconciled with what existed before it.

**1. Instructions a stack names still compose alphabetically, not in manifest order — that part
is unchanged.** `Resolve` (`source/toolkit/internal/resolver/resolver.go`) builds `defs` from
the overlay map and unconditionally `sort.Slice`s it by `(Category, Name)`. `StackOrder` (the
entry-visit order) is on the Plan but instruction rendering still never consults it for anything
a stack names by hand — listing `instructions: [zzz-first, aaa-second]` in that order still
renders `aaa-second` before `zzz-first`.

What changed: `local.instructions` is now the first and only path where an instruction's
*authored* order — its filename at scan time — is load-bearing. `scanLocalCategory`
(`resolver/local.go`) stamps a 1-based `ScanOrder` on each locally-scanned instruction, and
`orderInstructions` (duplicated once per adapter package, in `claude/instructions.go` and
`codex/agentsmd.go`) partitions the received list into `local.context`'s instruction (first,
identified by `StackName == "local.context"`), then everything a stack named (the existing
alphabetical order, untouched), then `local.instructions` re-sorted by `ScanOrder` (last). So
the alphabetical rule above is still true for the middle group and only the middle group.

**2. `rules:` never gets inlined body text into AGENTS.md or CLAUDE.md, and that is still true
now that `local.rules` can scan a directory of them.** For either platform, a rule is always a
separate whole-owned file (`source/toolkit/internal/adapters/claude/files.go` →
`.claude/rules/<name>.md`; `source/toolkit/internal/adapters/codex/files.go` →
`.agents/rules/<name>.md`) — a locally-scanned rule goes through the identical
`s.parseFromFS`/overlay path as one a stack names, so it renders exactly the same way. Claude's
adapter never emits a "## Rules" heading; only **codex** synthesizes one plus a sorted index of
links, because "Codex has no rules-discovery mechanism of its own." A local rule slots into that
same generated index like any other — no separate code path exists for it, and none was added.

For codex, AGENTS.md is a `fsops.SingleFileOp` — a whole-owned file fully regenerated every
render, not a managed-region file like CLAUDE.md. `render-refuses-files-it-does-not-track`
means the *first* render onto an untracked, hand-written AGENTS.md is refused outright; once
tracked, every render overwrites the whole file from `buildAgentsMD`'s template. Adding
`local.rules` did not change any of this — it feeds the same rules slice `buildAgentsMD` already
walked.
