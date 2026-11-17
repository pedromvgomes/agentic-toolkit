---
about: "older-agtk hook gotcha holds; the handoff hook paragraph is gone, cloud-init is the second example"
saw:
  - definitions/hooks/no-authoring-footers-claude-pre-tool-use.yaml
  - definitions/hooks/cloud-init-claude-session-start.yaml
targets: hook-must-tolerate-an-older-agtk
verdict: still-true
---
Rewrite needed for the last paragraph only. The general claim holds:
`no-authoring-footers-claude-pre-tool-use.yaml:15` runs `agtk guard footers --help >/dev/null 2>&1 || exit 0`
before `agtk guard footers` at `:16`. `handoff-claude-session-start.yaml` no longer exists, so
"guards only `command -v agtk` (line 27)" is false and should be deleted.

The second example now is `cloud-init-claude-session-start.yaml:24`:
`agtk cloud init --help 2>&1 | grep -q -- '--render' || exit 0`, with a comment at `:21` that a binary
without `cloud init` answers with the parent help (so a plain exit-status probe would not detect it,
and the probe greps for the flag the hook needs). That is the stricter form of the same rule.
