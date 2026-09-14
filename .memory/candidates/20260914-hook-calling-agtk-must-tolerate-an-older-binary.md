---
about: a hook that shells out to an agtk subcommand must tolerate an installed agtk older than the stack that rendered the hook
saw:
  - definitions/hooks/no-authoring-footers-claude-pre-tool-use.yaml
  - definitions/hooks/handoff-claude-session-start.yaml
  - source/toolkit/internal/cli/guard.go
---

A stack's hooks come from whatever ref of the catalog a consumer renders, while the `agtk` on
PATH is whatever they last installed, so the hook can name a subcommand the binary does not
have. `command -v agtk` only covers the binary being absent: with agtk 0.14.0 installed,
`agtk guard footers` fails with `unknown command "guard"` (exit 1), and a `PreToolUse` hook on
`Bash` would surface that as a hook error on every Bash call.

`definitions/hooks/no-authoring-footers-claude-pre-tool-use.yaml` therefore probes the
subcommand itself — `agtk guard footers --help >/dev/null 2>&1 || exit 0` — which exits 1 for
an unknown subcommand and 0 when it exists (checked against the installed 0.14.0 and a build
of this branch), and also covers a missing binary (exit 127). The real call then runs with its
exit status propagating, so exit 2 from `agtk guard footers` still blocks.

`handoff-claude-session-start.yaml` guards only `command -v agtk`, so it has the same exposure
for any agtk that predates `agtk handoff list`.
