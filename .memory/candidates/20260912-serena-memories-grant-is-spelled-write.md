---
about: skill-permissions.yaml grants Serena's memories directory with Write(...), the spelling the file permission check never consults
saw:
  - definitions/settings/skill-permissions.yaml
  - source/toolkit/internal/curator/curator.go
  - source/toolkit/internal/adapters/claude/settings.go
targets: curator-write-grant-is-spelled-edit-with-no-mode
verdict: still-true
---

The note's spelling claim holds, and it now has a third site — one of which is still wrong.

`source/toolkit/internal/curator/curator.go:238-241` states it: "an Edit rule covers every
file-editing tool including Write, while a Write rule is not consulted by the file permission
check at all — so a grant written the obvious way names the right path and constrains
nothing." The curator spells its own grant `Edit(...)` (`:249`).

`source/toolkit/internal/adapters/claude/settings.go` builds the memory store's grants and
spells the staging one `Edit(...)` for the same reason, with a test asserting no emitted grant
starts with `Write(`.

`definitions/settings/skill-permissions.yaml:7` still reads:

    - "Write(./.agents/memories/**)"

That is Serena's memories directory, not the memory store, and it is the one remaining grant
written the way the note says constrains nothing. It pre-approves nothing, so whatever writes
there prompts on every use while the settings file reads as though it does not. It fails safe
— an extra prompt, never an over-grant — which is why nothing has surfaced it.

Worth a note of its own or a line in this one: the spelling rule now governs three places and
is enforced by a test in only two.
