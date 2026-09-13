---
about: glob-pattern EntryRef support has no prior discussion anywhere in the repo, and an entry manifest that sets memory:/local: cannot itself be the target of someone else's extends:
saw:
  - docs/adr/*.md
  - source/toolkit/internal/stack/parser.go
  - source/toolkit/internal/resolver/resolver.go
---

Two findings from the same pass, both took real digging (ADR sweep, full git log grep,
tracing how `ctx.Identifier` gets set).

**Glob entries**: `grep -rn glob docs/adr/*.go source/toolkit/internal/stack/*.go
source/toolkit/internal/resolver/*.go` only turns up `docs/adr/0005-glob-anchors-mark-
quantified-claims.md`, which is about memory-note anchors, unrelated to `EntryRef`.
`git log --all --oneline -i --grep="glob"` and `--grep="wildcard"` across the whole repo
history return no commit touching entry resolution. `EntryRef` parsing
(`source/toolkit/internal/stack/parser.go:156`, `ParseEntryRef`) has never had glob syntax
added or removed — this is "never proposed," not "considered and rejected." A redesign
proposing glob support on `EntryRef` is not overriding a prior decision; there is none to
override.

**Entry manifest cannot be extended by another stack today**: `Resolve`
(`source/toolkit/internal/resolver/resolver.go:33-51`) hard-codes `Identifier: ""` for
whatever `*stack.Stack` its caller passes in as `entry` — this is a property of *how a struct
was passed into Resolve*, not of the struct itself. A `.agentic-toolkit.yaml`-shaped file
reached instead via `extends:` (`loadExtends` → `ParseInFS`, `resolver.go:250+`) gets a
non-empty identifier like any other extended stack. If that file sets `memory:` it gets the
soft diagnostic (`resolver.go:193`); if it sets `local:` it hits the hard refusal
(`resolver.go:203-206`, per ADR 0016). So an entry manifest with either field set genuinely
cannot be the target of someone else's `extends:` without failing — nothing marks the type as
"entry-only," the entry/extended distinction is purely which argument position a stack was
passed in at `Resolve` call time, enforced after the fact by the two field-specific checks.
