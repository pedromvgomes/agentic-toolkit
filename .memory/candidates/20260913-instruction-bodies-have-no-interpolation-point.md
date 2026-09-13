---
about: definition bodies (instructions, skills, agents, commands, rules) render byte-for-byte verbatim; the memory-root permission grant added in #100 is not a precedent for interpolating one, because it injects into a structured JSON merge, not markdown text
saw:
  - source/toolkit/internal/adapters/claude/instructions.go
  - source/toolkit/internal/adapters/claude/files.go
  - source/toolkit/internal/definitions/parser.go
  - source/toolkit/internal/adapters/claude/settings.go
---

`buildInstructionsRegion` (instructions.go:104-121) does
`body := strings.TrimSpace(inst.Body)` then writes it straight into the managed region — no
`text/template`, no placeholder substitution, anywhere in the concatenation path. The same is
true for the other body-carrying categories: `parser.go:211-224` (`setBody`) assigns the parsed
markdown body string as-is, and `files.go`'s `frontmatterPlusBody` (:148-160-ish) just appends
frontmatter YAML + the body string unchanged. `grep -rn "text/template" internal/adapters
internal/definitions internal/resolver` finds nothing. There is no ADR or comment stating
"bodies render verbatim" as a deliberate invariant — it is simply the only code path that
exists; nothing was found that considered and rejected templating.

The memory-root permission grant (`addMemoryGrants`, settings.go:283-... , from commit 7607c87 /
PR #100) is often read as precedent for "agtk can inject a per-consumer fact into shared
content at render time." It is not the same shape: `renderSettings` builds `settings.json` as a
merged Go `map[string]any`, so the adapter can programmatically mutate one specific key
(`permissions.allow`) after the definition-authored fragments are merged in. An instruction's
body is a single opaque string with no equivalent structured slot to reach into — there is
nothing in the render pipeline analogous to `perms[allowKey] = allow` for markdown prose. The
settings.go comment (`addMemoryGrants` doc, settings.go ~277-282) states the reason it exists
at all: "the value a definition carries is opaque to the merge, so nothing along that route can
substitute the root either" — which is exactly the wall a body-templating feature would hit,
stated by the mechanism that had to route around it a different way (structured JSON, not text).

So a new instruction whose body must name a per-consumer path (e.g. `local.instructions`) has
no existing render-time interpolation mechanism to reuse; it would need either a wholly new one
(templating on `Instruction.Body` at render time) or to phrase the instruction generically
("the folder your `.agentic-toolkit.yaml` declares") and let the agent go look it up, rather
than naming the path inline.
