---
name: hook-fail-closed-is-never-rendered
kind: gotcha
description: A hook's fail_closed parses and validates but no adapter ever writes it, so every rendered hook is fail-open.
anchors:
  - path: source/toolkit/internal/definitions/types.go
    blob: ee984767cd43
  - path: definitions/SCHEMA.md
    blob: 759e101e6d39
  - path: source/toolkit/internal/adapters/claude/*.go
    matches:
      - path: source/toolkit/internal/adapters/claude/diff.go
        blob: fc92adfaf76d
      - path: source/toolkit/internal/adapters/claude/files.go
        blob: ee7d951bccd8
      - path: source/toolkit/internal/adapters/claude/instructions.go
        blob: 39b42509cf63
      - path: source/toolkit/internal/adapters/claude/mcp.go
        blob: 69ecb724a03f
      - path: source/toolkit/internal/adapters/claude/render.go
        blob: 4a87de9b70a9
      - path: source/toolkit/internal/adapters/claude/settings.go
        blob: a76ce7243f96
confidence: verified
---

`Hook.FailClosed` is declared at `source/toolkit/internal/definitions/types.go:275`
(`yaml:"fail_closed,omitempty"`) and documented at `definitions/SCHEMA.md:249` — "If true, a
non-zero exit blocks the action; default is fail-open."

Nothing renders it. `collectHooks` (`source/toolkit/internal/adapters/claude/settings.go:202`) is the only
place a `Hook` becomes a Claude `hooks` entry, and it reads `h.Event`, `h.Matcher`,
`h.Handler.{Type,Command,Prompt,Model}` and `h.Timeout` (`:226`) — never `h.FailClosed`.
Repo-wide, the identifier appears only at that one declaration; the string `fail_closed`
otherwise appears only in the schemagen example (`source/toolkit/tools/schemagen/main.go:103`), the generated
docs, and a test fixture. `source/toolkit/internal/adapters/` holds exactly one adapter (`claude`), so there
is no second renderer picking it up.

So `fail_closed: true` parses cleanly, validates, and silently does nothing: the rendered
`.claude/settings.json` entry carries no field controlling it, and the platform's own
fail-open default governs. A hook that must block on failure has to encode that in the
command itself, not in this field.

The glob anchor is deliberate: the claim is that *no* file in the adapter writes this field,
which a newly added file could falsify invisibly to a per-file anchor.
