---
about: a stack's platforms: field is read only off the entry manifest's own parsed struct, with no diagnostic when set on a stack reached through extends: — a stricter silence than memory: gets
saw:
  - source/toolkit/internal/cli/render.go
  - source/toolkit/internal/cli/lock.go
  - source/toolkit/internal/resolver/resolver.go
  - source/toolkit/internal/stack/types.go
---

`runRender` (source/toolkit/internal/cli/render.go:59) gets `st` from `loadStack(env)`
(source/toolkit/internal/cli/lock.go:143-149), which is `stack.ParseFile(path)` on the entry
manifest alone — one YAML file, not a merged view of the extends graph. `renderPlatforms`
(render.go:85-87) then calls `st.EffectivePlatforms()` on that same object
(stack/types.go:117-121). So `platforms:` written on any stack reached through `extends:` is
never read by anything: the resolver's traversal (`resolver.go`'s `loadStack`, the DFS over
`st.Extends`) never inspects `.Platforms` at all — contrast with `.Memory`, which the same
traversal explicitly checks per-node (`resolver.go:191`) and reports via
`DiagIgnoredMemoryConfig` (resolver/types.go:142-146) when set outside the entry. `platforms:`
gets no equivalent diagnostic; it just silently does nothing, one level more silent than the
precedent memory: already set.

This means "entry-manifest-only" isn't one designed rule applied consistently — `memory:` is
enforced-with-diagnostic inside the shared traversal, `platforms:` is enforced by construction
(nothing ever hands an extended stack's struct to `EffectivePlatforms()`) with no diagnostic
at all. Two different mechanisms landing on the same policy, not one mechanism reused.
