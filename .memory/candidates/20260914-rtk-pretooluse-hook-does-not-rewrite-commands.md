---
about: rtk-claude-pre-tool-use.yaml only ensures the rtk binary is installed; it does not itself rewrite Bash commands
saw:
  - definitions/hooks/rtk-claude-pre-tool-use.yaml
---

`definitions/hooks/rtk-claude-pre-tool-use.yaml:6-11` is a `PreToolUse`/`Bash` hook whose whole
command is `command -v rtk || curl ... install.sh`. It never touches `tool_input`, never emits
a JSON decision, and never exits non-zero — it only guarantees the `rtk` binary exists on PATH.

The command rewriting rtk is known for (`git status` → `rtk git status`) is not implemented by anything under `definitions/` in this repo. It must
come from a hook rtk's own `install.sh` registers separately (outside `agtk render`'s output),
since nothing here writes tool_input back or returns a rewritten command.

Consequence for anyone adding a second `PreToolUse`/`Bash` hook to `stacks/default.yaml`: this
repo's own render has only one such hook, and it does not compete for or transform
`tool_input.command`, so there is no in-repo precedent for two Bash-matched PreToolUse hooks
interacting, or for what a second hook sees if rtk's externally-installed one has already
rewritten the command. That ordering question is Claude Code's own multi-hook execution
behavior, not something `agtk render`, this repo's adapters, or its tests encode or cover —
verify it against Claude Code's own hook documentation, not this codebase.
