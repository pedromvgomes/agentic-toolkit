---
about: a hook's command runs outside the Bash tool's permission system, so it needs no Bash(...) allow grant
saw:
  - definitions/hooks/handoff-claude-session-start.yaml
  - definitions/settings/memory-permissions.yaml
  - definitions/settings/skill-permissions.yaml
  - source/toolkit/internal/cli/tests/default_stack_render_test.go
---

`handoff-claude-session-start.yaml`'s command invokes `agtk handoff list` directly, but no
`settings/*.yaml` definition pre-approves `Bash(agtk handoff list*)`, and
`TestPreApprovedPermissionsNameCommandsThatExist`
(source/toolkit/internal/cli/tests/default_stack_render_test.go:105) only asserts pre-approval
for commands an *agent* runs through the Bash tool (`agtk memory stats`, `agtk memory show`,
`agtk memory candidates`) — never for a hook's own command.

By contrast every `agtk <subcmd>` an agent is expected to invoke via Bash gets a matching
`Bash(agtk ...*)` entry in `definitions/settings/{memory,skill}-permissions.yaml`.

So a hook's `handler.command` is not subject to the same-session Bash allow/deny prompt at
all — it is Claude Code's own hook runner executing a fixed shell string, not a tool call the
model asked to make. A new hook whose command shells out to a new `agtk` subcommand needs no
settings-definition permission grant; only an `agtk` invocation an *agent* is meant to issue
via Bash does.
