---
about: memory.root is honoured only in the entry manifest because the resolver tests the traversal context's Identifier for emptiness, not because of anything YAML-schema-level
saw:
  - internal/resolver/resolver.go
  - internal/resolver/types.go
  - internal/stack/types.go
---

Asked while designing `.agents/code-review/manifest.yaml`, which deliberately mirrors the
memory subsystem's conventions — this is the rationale and exact enforcement point for one
of them, not previously in the store.

The entry stack is given `Identifier: ""` at `internal/resolver/resolver.go:49` (the
`stackCtx` built in `Resolve`). Every stack reached through `extends:` gets a non-empty
identifier built from its source URL (`resolver.go:260`, `:279`, `:360`, all setting
`Identifier: identifier` from a resolved source, never `""`).

`loadStack` (`resolver.go:190-197`) checks exactly that flag:

```go
if ctx.Identifier != "" && st.Memory != nil {
    s.diags = append(s.diags, Diagnostic{
        Kind: DiagIgnoredMemoryConfig,
        Message: fmt.Sprintf("stack %q sets memory:, which is honoured only in the entry manifest; ignoring it",
            displayID(ctx.Identifier)),
        ...
    })
}
```

So a non-entry stack's `memory:` block is parsed successfully (the field exists on
`stack.Stack`, `internal/stack/types.go:63`) but silently ignored except for one
informational diagnostic — it is never a hard error. `DiagIgnoredMemoryConfig` is declared at
`internal/resolver/types.go:141-145` with the stated reason: "a remote stack must not
relocate a consumer's committed notes, and it must not hard-fail the consumer's build
either."

`stack.Stack.MemoryRoot()` (`internal/stack/types.go:79`) itself has no notion of
entry-vs-extended — it just returns whatever `Memory.Root` is on the struct it's called on.
The entry-only rule lives entirely in the resolver's traversal, not in the accessor or the
type. A subsystem mirroring this convention (e.g. `code-review:` in a new manifest) would
need the equivalent of `ctx.Identifier != ""` at the same point in its own traversal, since
nothing enforces the restriction below that layer.
