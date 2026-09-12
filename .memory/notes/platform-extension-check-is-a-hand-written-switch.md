---
name: platform-extension-check-is-a-hand-written-switch
kind: invariant
description: Every extension pointer field in definitions/types.go must have a line in presentExtensions; omitting one silently disables the platform check for it.
anchors:
  - path: source/toolkit/internal/definitions/parser.go
    blob: c59af791af5f
  - path: source/toolkit/internal/definitions/types.go
    blob: ee984767cd43
confidence: verified
---

`validateExtensionsAgainstPlatforms` (`source/toolkit/internal/definitions/parser.go:415`) enforces that a
populated `extensions.<platform>` block names a platform the definition's `platforms:` list
allows. It gets the set of populated blocks from `presentExtensions`
(`source/toolkit/internal/definitions/parser.go:433`), a type switch that names each pointer field by hand:

    case *Agent:
        m[PlatformClaude]   = d.Extensions.Claude != nil
        m[PlatformCursor]   = d.Extensions.Cursor != nil
        m[PlatformOpenCode] = d.Extensions.OpenCode != nil

The invariant: **every `*<Platform><Category>Ext` pointer field declared on an extensions
struct in `source/toolkit/internal/definitions/types.go` must have a line in `presentExtensions`.** As of
this reading all do — Skill/Claude (`types.go:133`), Rule/Cursor (`:156`),
Agent/{Claude,Cursor,OpenCode} (`:207`), Command/{OpenCode,Copilot} (`:251`),
Hook/{Claude,Cursor} (`:297`), MCPServer/{Claude,OpenCode} (`:366`); Instruction and Setting
declare no extensions at all. The claim quantifies over the extension structs in that one
file and is falsified by the next field added there, which is why `types.go` is anchored.

What breaks: adding, say, `CopilotAgentExt` to `AgentExtensions` and forgetting the
`presentExtensions` line compiles, decodes, and validates clean. There is no compile error
(the map is built from literals, not from the struct), no default case, and no test that
enumerates struct fields. The block is simply never checked, so `platforms: [claude]` plus a
populated `extensions.copilot` parses successfully and the adapter downstream is handed an
extension for a platform the definition says it does not target.

Second half of the gate: the check runs **only** when `platforms:` is non-empty
(`parser.go:404`). A definition with no `platforms:` list may carry any extension block — that
is deliberate, since `platforms:` is a narrowing allowlist, but it means the omission above is
invisible on the majority of definitions that leave `platforms:` unset.

The comment at `parser.go:430-432` says the switch avoids reflection "to keep the schema
definition (struct shape) the only source of truth" — in practice the switch *is* a second
source of truth, and it is the one that decides whether the check happens.
