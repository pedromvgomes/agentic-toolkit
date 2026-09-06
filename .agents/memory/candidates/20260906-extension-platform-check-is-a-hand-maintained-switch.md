---
about: presentExtensions is a hand-written switch that must mirror every extension pointer field, and omitting one silently disables the platform check
saw:
  - internal/definitions/parser.go
  - internal/definitions/types.go
---

Kind: **invariant**.

`validateExtensionsAgainstPlatforms` (parser.go:415) enforces that a populated
`extensions.<platform>` block names a platform the definition's `platforms:` list allows.
It gets the set of populated blocks from `presentExtensions` (parser.go:433), a type switch
that names each pointer field by hand:

    case *Agent:
        m[PlatformClaude]   = d.Extensions.Claude != nil
        m[PlatformCursor]   = d.Extensions.Cursor != nil
        m[PlatformOpenCode] = d.Extensions.OpenCode != nil

The invariant: **every `*<Platform><Category>Ext` pointer field declared on an extensions
struct in internal/definitions/types.go must have a line in `presentExtensions`.** As of
this reading all of them do — Skill/Claude, Rule/Cursor, Agent/{Claude,Cursor,OpenCode},
Command/{OpenCode,Copilot}, Hook/{Claude,Cursor}, MCPServer/{Claude,OpenCode}; Instruction
and Setting declare no extensions at all. That is a claim quantified over the extension
structs in `internal/definitions/types.go` and is falsified by the next field added there.

What breaks when someone gets this wrong:

Adding, say, `CopilotAgentExt` to `AgentExtensions` and forgetting the `presentExtensions`
line compiles, decodes, and validates clean. There is no compile error (the map is built
from literals, not from the struct), no default case, and no test that enumerates struct
fields. The block is simply never checked, so `platforms: [claude]` plus a populated
`extensions.copilot` parses successfully and the adapter downstream is handed an extension
for a platform the definition says it does not target.

Note the second half of the gate: the check runs **only** when `platforms:` is non-empty
(parser.go:404). A definition with no `platforms:` list may carry any extension block — that
is deliberate, since `platforms:` is a narrowing allowlist, but it means the omission above
is invisible on the majority of definitions that leave `platforms:` unset.

The comment at parser.go:431 says the switch avoids reflection "to keep the schema
definition (struct shape) the only source of truth" — in practice the switch *is* a second
source of truth, and it is the one that decides whether the check happens.
