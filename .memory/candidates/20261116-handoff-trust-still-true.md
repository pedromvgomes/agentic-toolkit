---
about: "handoff trust note is obsolete: agtk handoff, its skills and hook are deleted"
saw:
  - docs/adr/0014-a-handoff-is-trusted-structurally-not-by-prose.md
  - docs/adr/0028-a-backlog-item-is-trusted-by-who-wrote-it.md
  - source/toolkit/internal/backlog
targets: handoff-trust-is-asked-of-git
verdict: now-false
---
Every anchor of the note except ADR 0014 is deleted on this branch: `internal/handoff/handoff.go`,
`internal/cli/handoff.go`, `definitions/hooks/handoff-claude-session-start.yaml`,
`definitions/skills/implement-handoff/`. The `git ls-files -o` untracked-means-locally-written trust
rule has no caller left. ADR 0014 is superseded by ADR 0028, where trust is by who wrote the backlog
item (`internal/backlog`, `agtk backlog verify`), not by git tracking state.

Obsolete: the note should be retired. Nothing in it is worth rewriting; the four git case/Unicode
folds it lists were specific to the deleted filesystem check.
