---
description: AGENTS.md and CLAUDE.md are rendered output — edit the source instruction, not the generated file.
---

## Instructions are rendered, not edited

AGENTS.md and CLAUDE.md are rendered output. Editing either file directly loses the edit the
next time `agtk render` runs, because the render overwrites its managed region from the
definitions and stacks it was given, not from what a prior render left behind.

To add an instruction, add a file instead, in the location your repo's
`.agentic-toolkit.yaml` declares under `local.instructions`. Then run `agtk render` — `render`
is enough because a local file needs no network fetch; reach for `agtk sync` only when a
remote source also needs refreshing. The new file has no effect until it's rendered.
