---
about: "no-Bash-grant-for-hook-commands note: handoff example deleted, cloud-init hook is the live instance"
saw:
  - definitions/hooks/cloud-init-claude-session-start.yaml
  - definitions/settings/memory-permissions.yaml
  - definitions/settings/skill-permissions.yaml
  - source/toolkit/internal/cli/tests/default_stack_render_test.go
targets: hook-commands-need-no-bash-grant
verdict: still-true
---
The subject `handoff-claude-session-start.yaml` / `agtk handoff list` is deleted, so the note's
example must be rewritten; the observation survives with a new instance. `cloud-init-claude-session-start.yaml`
runs `agtk cloud init --render` (`:24`, `:30`) and `grep -rn cloud definitions/settings/*.yaml` finds
no `Bash(agtk cloud ...)` grant (only a comment in `no-attribution.yaml:8`). Grants that do exist:
`memory-permissions.yaml:7-12` and `skill-permissions.yaml:18-20`. The test is now
`TestPreApprovedPermissionsNameCommandsThatExist` at `default_stack_render_test.go:212` (note said :168).

Confidence stays `suspect`: the hook-runner-is-not-subject-to-allow-list reasoning is Claude Code
behaviour, still not established by anything in this repo.
