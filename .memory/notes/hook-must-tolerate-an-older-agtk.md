---
name: hook-must-tolerate-an-older-agtk
kind: gotcha
description: A rendered hook that calls an agtk subcommand must probe that the subcommand exists, because the installed binary can be older than the catalog ref that rendered the hook.
anchors:
  - path: definitions/hooks/no-authoring-footers-claude-pre-tool-use.yaml
    blob: 2d3e57947d13
  - path: definitions/hooks/handoff-claude-session-start.yaml
    blob: d5d8a261ef6f
confidence: verified
---

Hooks come from whatever catalog ref a consumer renders; the `agtk` on PATH is whatever they last
installed. `command -v agtk` covers only an absent binary. An older binary lacking the subcommand
exits 1 with an unknown-command error, which on a `PreToolUse` hook for `Bash` would surface as a
hook error on every Bash call.

`definitions/hooks/no-authoring-footers-claude-pre-tool-use.yaml` therefore runs
`agtk guard footers --help >/dev/null 2>&1 || exit 0` first (its own comment explains why), which
also covers a missing binary (127), and only then runs the real call so its exit status
propagates (exit 2 still blocks).

`definitions/hooks/handoff-claude-session-start.yaml` guards only `command -v agtk` (line 27), so
it has the same exposure for any agtk that predates `agtk handoff list`.
