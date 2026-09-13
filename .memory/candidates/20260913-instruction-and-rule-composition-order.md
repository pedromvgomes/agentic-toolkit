---
about: instructions render alphabetically by declared name today, not in manifest list order, and rules never inline into AGENTS.md/CLAUDE.md — they render as separate files, with codex synthesizing a literal "## Rules" index heading that Claude's adapter never emits
saw:
  - source/toolkit/internal/resolver/resolver.go
  - source/toolkit/internal/adapters/claude/instructions.go
  - source/toolkit/internal/adapters/claude/files.go
  - source/toolkit/internal/adapters/codex/agentsmd.go
  - source/toolkit/internal/adapters/codex/files.go
---

Explored for a feature adding `local: {instructions: dir, ...}` directory scanning with
filename-order composition. Two things a directory-scan-by-filename-order design needs to
reconcile with what exists today.

**1. Manifest-listed instructions do not compose in manifest order at all.** `Resolve`
(`source/toolkit/internal/resolver/resolver.go:64-83`) builds `defs` from the overlay map and
then unconditionally `sort.Slice`s it by `(Category, Name)` — alphabetical by the winning
definition's declared name within a category. `StackOrder` (the entry-visit order) is recorded
on the Plan (`resolver.go:91`) but instruction rendering never consults it.
`buildInstructionsRegion` (`source/toolkit/internal/adapters/claude/instructions.go:104-121`)
and `buildAgentsMD` (`source/toolkit/internal/adapters/codex/agentsmd.go:16-30`) both just walk
`plan.Definitions`/the filtered instruction slice in the order they arrive, which is that
alphabetical-by-name order. The comment at `instructions.go:102` ("Order matches
plan.Definitions (alphabetical by name within instruction category)") is accurate as of this
read. So today, listing `instructions: [zzz-first, aaa-second]` in that order in the manifest
still renders `aaa-second` before `zzz-first` — the manifest's own sequence is not
load-bearing. A directory scan ordered by filename would be the *first* place instruction
order is actually authored by sequence rather than by name; it does not need to fight an
existing "manifest order wins" behavior because there isn't one.

**2. `rules:` never gets inlined body text into AGENTS.md or CLAUDE.md at all** — for either
platform, a rule is always a separate whole-owned file
(`source/toolkit/internal/adapters/claude/files.go:45-51` →
`.claude/rules/<name>.md`; `source/toolkit/internal/adapters/codex/files.go:53-60` →
`.agents/rules/<name>.md`). Claude's adapter (`claude/instructions.go`) never emits any
"## Rules" heading anywhere — Claude Code's own rules-discovery presumably finds
`.claude/rules/` directly. Only the **codex** adapter synthesizes a `## Rules` heading plus a
sorted index of links (`codex/agentsmd.go:32-44`, literal string at line 40), because "Codex
has no rules-discovery mechanism of its own" (comment at `agentsmd.go:14-15`, restated at
`codex/files.go:109`). So the premise in the question ("`rules:` auto-generates its own
`## Rules` section inside AGENTS.md") is true only for the codex platform, not universally.

For codex specifically, AGENTS.md is a `fsops.SingleFileOp` (`codex/files.go:86`) — a
whole-owned file fully regenerated every render, not a managed-region file like CLAUDE.md
(contrast with `claude/instructions.go`'s marker-delimited region). That means a hand-written
AGENTS.md with a pre-existing `## Rules` heading cannot silently collide with the generated
one: `render-refuses-files-it-does-not-track` (existing note) means the *first* render onto an
untracked, hand-written AGENTS.md is refused outright, not merged. Once AGENTS.md is
manifest-tracked, every subsequent render overwrites the whole file from `buildAgentsMD`'s
template, so there is no duplicate-heading scenario possible — the file is either refused or
fully replaced, never partially merged. This present-day mechanism does not itself address
what a *local rules directory* declared for codex should do to that same generated heading,
since scanned rules do not exist yet.
