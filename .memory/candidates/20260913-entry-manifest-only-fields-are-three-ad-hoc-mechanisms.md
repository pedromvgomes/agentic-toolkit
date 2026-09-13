---
about: Stack has three entry-manifest-only fields (memory, local, platforms), each enforced a different way, with no shared mechanism
saw:
  - source/toolkit/internal/resolver/resolver.go
  - source/toolkit/internal/stack/types.go
  - source/toolkit/internal/cli/render.go
targets: memory-config-is-entry-manifest-only
verdict: still-true
---

Re-checked because the note was stale (resolver.go, types.go both moved since the note's
anchors were taken).

`memory:` enforcement: `if ctx.Identifier != "" && st.Memory != nil` at
`source/toolkit/internal/resolver/resolver.go:193` — an informational diagnostic
(`DiagIgnoredMemoryConfig`), non-fatal. Line moved from the note's `:191` to `:193`; claim
still holds.

`local:` enforcement, added since the note was written: `if ctx.Identifier != "" &&
st.Local != nil` at `resolver.go:203-206` — a hard error, not a diagnostic. Documented as a
deliberate asymmetry in `docs/adr/0016-local-is-entry-manifest-only-and-refuses-elsewhere.md`.

`platforms:` has no enforcement at all in the resolver — `grep -n Platforms
source/toolkit/internal/resolver/*.go` returns nothing. It is inert on an extended stack not
because anything ignores it, but because nothing ever looks at it there: the only reader,
`st.EffectivePlatforms()` at `source/toolkit/internal/cli/render.go:87`, is called on the
`*stack.Stack` `loadStack` (`source/toolkit/internal/cli/lock.go:143`) returns from
`stack.ParseFile` on the entry file alone — an extended stack's parsed struct is never
retained past the resolver's traversal, so its `Platforms` field is unreachable, not refused.

So this is three separate ad hoc mechanisms (diagnostic, hard error, silent unreachability),
not one shared "entry-manifest-only" enforcement point. Every field on `stack.Stack` besides
these three (`Root`, `Extends`, the eight `EntryRef` category lists) is legitimately usable on
an extended stack — `Memory`, `Local` and `Platforms` are the complete set with
entry-manifest-only semantics, confirmed by reading every field on the struct
(`source/toolkit/internal/stack/types.go:51-70`).
