---
about: a file-path permission grant cannot govern what an MCP server does in its own process
saw:
  - definitions/settings/skill-permissions.yaml
  - source/toolkit/internal/curator/curator.go
  - definitions/mcp/serena.yaml
  - source/toolkit/internal/cli/tests/memory_stack_render_test.go
targets: curator-write-grant-is-spelled-edit-with-no-mode
verdict: now-false
---

`definitions/settings/skill-permissions.yaml` used to carry
`Write(./.agents/memories/**)`, meant to pre-approve Serena writing its memories.
`grep -rn memories definitions/` now finds nothing — the grant is gone (removed in PR #101,
released as v0.13.0). The prior candidate's `still-true` verdict on that grant no longer holds.

That candidate framed the grant as a spelling bug: `Write(...)` where `Edit(...)` was meant,
per the rule stated in the comment above the curator's own grant
(`source/toolkit/internal/curator/curator.go:238`, "an Edit rule covers every file-editing
tool including Write, while a Write rule is not consulted by the file permission check at
all"). Respelling it would not have fixed it. The grant had three independent defects:

- the spelling defect above
- `.agents/memories/` names a path that exists nowhere — Serena keeps its memories at
  `.serena/memories/`, never under `.agents/`
- `.agents/` is the codex adapter's rendered output tree: gitignored and pruned on every
  render, so a grant anchored there cannot survive even if the path existed

The deciding fact under all three: nothing in the catalog writes Serena memories through
Claude's file tools in the first place. Serena writes them itself, in its own process,
through its MCP server's `write_memory` (`definitions/mcp/serena.yaml` declares the server;
the tool is Serena's, not a file write Claude performs). Claude's file-permission rules —
`Edit(...)`/`Write(...)` path globs — are consulted only for Claude's own file-editing tools.
An MCP server's own tool calls are governed by `mcp__<server>__*`-shaped rules instead,
independent of any path grant.

The durable lesson: before adding a path-shaped grant for something "written to disk",
establish which mechanism actually performs the write. If it is an MCP server acting in its
own process, no `Edit(...)`/`Write(...)` grant reaches it regardless of spelling or path —
the rule needed is an `mcp__<server>__*` rule, not a file-path one.

The catalog now has a standing guard against a version of this recurring:
`TestNoGrantNamesTheRenderedAgentsTree` in
`source/toolkit/internal/cli/tests/memory_stack_render_test.go:116` renders both the
`default` and `memory` stacks and fails if any allow-list rule contains `.agents/` or is
spelled `Write(...)`. The spelling half of that check previously existed only at the two
places agtk *builds* a grant in Go (the curator itself, and
`source/toolkit/internal/adapters/claude/settings.go`) — never against what the catalog
*ships*. This test is the first guard at the catalog level.
