---
name: hook-commands-need-no-bash-grant
kind: gotcha
description: A hook's handler.command is run by the harness, not the model, so it needs no Bash(...) allow entry; only agtk commands an agent itself issues do.
anchors:
  - path: definitions/hooks/handoff-claude-session-start.yaml
    blob: d5d8a261ef6f
  - path: definitions/settings/memory-permissions.yaml
    blob: 477cbbdf0a0b
  - path: definitions/settings/skill-permissions.yaml
    blob: 9f7ed4061000
  - path: source/toolkit/internal/cli/tests/default_stack_render_test.go
    blob: 4bdc477907b9
confidence: suspect
---

`handoff-claude-session-start.yaml` runs `agtk handoff list` directly, yet no
`definitions/settings/*.yaml` carries `Bash(agtk handoff list*)`. Every `agtk` subcommand an
agent is expected to run via Bash does have an entry (`memory-permissions.yaml:7-11`,
`skill-permissions.yaml:18-20`), and `TestPreApprovedPermissionsNameCommandsThatExist`
(`source/toolkit/internal/cli/tests/default_stack_render_test.go:168`) checks only those.

The inferred reason is that a hook command is a fixed shell string run by Claude Code's hook
runner, not a tool call subject to the allow/deny prompt. That is Claude Code behaviour and is
not established by anything in this repo, hence `suspect`: confirm against Claude Code's hook
documentation before relying on it.
