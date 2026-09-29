---
name: path-grants-do-not-reach-mcp-servers
kind: gotcha
description: An Edit/Write path grant governs only Claude's own file tools, never what an MCP server such as Serena writes in its own process, so a path grant for "memories" is dead weight.
anchors:
  - path: source/toolkit/internal/cli/tests/memory_stack_render_test.go
    blob: a58ef6ec2fbe
  - path: source/toolkit/internal/curator/curator.go
    blob: b7d9970d93f2
  - path: definitions/mcp/serena.yaml
    blob: a44db2ac7517
confidence: verified
---

`definitions/settings/skill-permissions.yaml` once carried `Write(./.agents/memories/**)` to
pre-approve Serena's memory writes; `grep -rn memories definitions` is now empty. It failed three
independent ways: `Write(...)` is not consulted by the file permission check (an `Edit` rule
covers `Write`; see the comment near `source/toolkit/internal/curator/curator.go:238` and
[[curator-write-grant-is-spelled-edit-with-no-mode]]); Serena keeps memories at
`.serena/memories/`, not under `.agents/`; and `.agents/` is the codex adapter's rendered,
gitignored, pruned-on-render tree (see [[committed-content-cannot-live-in-a-rendered-tree]]).

The deciding fact: Serena writes its memories itself, through its MCP server's `write_memory`
(server declared in `definitions/mcp/serena.yaml`). `Edit(...)`/`Write(...)` globs are consulted
only for Claude's own file-editing tools; an MCP server's tool calls are governed by
`mcp__<server>__*` rules. Before adding a path-shaped grant for something "written to disk",
establish which mechanism performs the write.

Guard: `TestNoGrantNamesTheRenderedAgentsTree`
(`source/toolkit/internal/cli/tests/memory_stack_render_test.go:116`) renders the `default` and
`memory` stacks and fails on any allow rule containing `.agents/` or spelled `Write(`. It is the
first check against what the catalog ships; before it, the spelling rule was only enforced where
agtk builds a grant in Go.
