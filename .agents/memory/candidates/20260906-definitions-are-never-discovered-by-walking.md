---
about: nothing walks definitions/ — every definition is reached by the path a stack manifest names, so walk.go is inert
saw:
  - internal/definitions/walk.go
  - internal/resolver/resolver.go
  - stacks/*.yaml
---

Kind: **gotcha**.

`internal/definitions/walk.go` reads like the catalog loader: `WalkCatalog` (walk.go:31)
scans `definitions/`, and `isEntryPoint` (walk.go:75) encodes which shape each category's
entry-point file has — `<name>/SKILL.md`, `<name>/AGENT.md`, flat `.md` for rules and
instructions, any-depth `.md` for commands, flat `.yaml`/`.yml` for hooks/mcp/settings.

None of it runs.

    grep -rn 'WalkCatalog\|ParseInCatalog' --include='*.go' .

`WalkCatalog` has no caller anywhere, production or test. `ParseInCatalog` (parser.go:19)
has no production caller either — only `internal/definitions/tests/parser_test.go`.

The real path is `resolver.parseFromFS` (internal/resolver/resolver.go:380), which calls
`definitions.ParseBundle` for skill/agent and `definitions.ParseFile` for everything else,
at a `(bundleDir, fileName)` computed from the entry a stack manifest listed
(resolver.go:314). `stacks/default.yaml` enumerates every skill, agent, command,
instruction, hook, mcp and setting by name.

What breaks when someone gets this wrong:

- Adding `definitions/skills/foo/SKILL.md` and expecting `agtk render` to pick it up. It
  is invisible until a stack lists `foo`. There is no discovery pass and no diagnostic —
  the file is simply never opened.
- Changing `isEntryPoint` to relax or tighten a category's file shape and expecting the
  loader to follow. The loader's shape rules live in `ParseBundle`'s hardcoded
  `SKILL.md`/`AGENT.md` (parser.go:53-62) and in the resolver's path layout, not here.
