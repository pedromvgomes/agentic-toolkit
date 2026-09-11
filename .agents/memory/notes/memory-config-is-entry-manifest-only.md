---
name: memory-config-is-entry-manifest-only
kind: gotcha
description: "A stack reached through extends: may set memory:, and it parses fine and is silently ignored — so a manifest's memory.root can disagree with agtk's."
anchors:
  - path: source/toolkit/internal/resolver/resolver.go
    blob: 022646e73709
  - path: source/toolkit/internal/resolver/types.go
    blob: f324cf4d5af4
  - path: source/toolkit/internal/stack/types.go
    blob: fe5cd947f8d3
confidence: verified
---

`memory:` is honoured only in the entry manifest, and the whole enforcement is one flag test
in the resolver's traversal: `if ctx.Identifier != "" && st.Memory != nil` at
`source/toolkit/internal/resolver/resolver.go:191`. The entry stack is the one built with `Identifier: ""`
(`resolver.go:49`); every stack reached through `extends:` gets a non-empty identifier from
its source URL.

The consequence is quiet. An extended stack's `memory:` block parses successfully — the field
exists on `stack.Stack` (`source/toolkit/internal/stack/types.go:63`) and the schema is strict, so nothing
rejects it — and the only trace is one informational diagnostic, `DiagIgnoredMemoryConfig`
(declared `source/toolkit/internal/resolver/types.go:142-146`, emitted `resolver.go:193`). It is never a hard
error, by design: "a remote stack must not relocate a consumer's committed notes, and it must
not hard-fail the consumer's build either."

So **do not read `memory.root` out of a manifest to learn where the store is** — the YAML and
`agtk` can disagree. Ask `agtk memory stats --json` for `root`.

`(*Stack).MemoryRoot()` (`source/toolkit/internal/stack/types.go:79`) has no notion of entry-vs-extended; it
returns whatever `Memory.Root` is on the struct it is called on. Nothing below the resolver's
traversal enforces the rule, so a subsystem that mirrors this convention needs its own
equivalent of the `ctx.Identifier != ""` test at the same point.
