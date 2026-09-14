---
description: AGENTS.md and CLAUDE.md are rendered output — edit the source instruction, not the generated file.
---

## Instructions are rendered, not edited

AGENTS.md and CLAUDE.md are rendered output. Editing either file directly loses the edit the
next time `agtk render` runs, because the render overwrites its managed region from the
definitions and stacks it was given, not from what a prior render left behind.

To add an instruction, add a markdown file under the entry manifest's `root:`-convention
instructions directory — `agentic/instructions/` when `root:` is left at its default — and run
`agtk render`. `render` is enough because a locally-authored file needs no network fetch;
reach for `agtk sync` only when a remote source reached through `stacks:` or `extends:` also
needs refreshing. The new file has no effect until it's rendered.
