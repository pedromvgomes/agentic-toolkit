---
about: "handoff trust note re-checked: code pointers hold, only the skill moved"
saw:
  - source/toolkit/internal/handoff/handoff.go
  - source/toolkit/internal/cli/handoff.go
  - definitions/hooks/handoff-claude-session-start.yaml
  - definitions/skills/implement-handoff/SKILL.md
  - docs/adr/0014-a-handoff-is-trusted-structurally-not-by-prose.md
targets: handoff-trust-is-asked-of-git
verdict: still-true
---
Stale only because SKILL.md changed. Re-checked: handoff.go List :59, Lstat :82, untrackedNames :188,
`git ls-files -z -o -- Dir` :194, hasExactly :225 all match; cli/handoff.go %q at :85/:88 (note said
:86,:89, off by one); hook calls `agtk handoff list` at :36; SKILL.md has it at :35 (note said :34).

Design consequence for a GitHub-issue handoff source (read from ADR 0014 + SKILL.md section 1): the
whole invariant is "locally written == untracked in git == a session on this machine wrote it". An
issue body has no such property: anyone with triage/write on the repo, or any commenter if the
dispatcher does not filter, authors it, and it still chooses tasks, file boundaries and commands for
subagents holding Write/Edit/Bash. ADR 0014 would need a successor defining the new trust root
(e.g. issue author/last editor must hold write permission, edits after labelling invalidate, body
read by id from the API not from a cached copy). `handoff.List`, the SessionStart hook and the
SKILL.md step 1 all assume a filesystem; none of it carries over.
