---
name: bash-writes-are-refused-inside-the-working-directory
kind: gotcha
description: A second guard, separate from the tool allowlist, can refuse a Bash command that creates a file inside the session's own working directory — so a constructed `Bash(...)` grant is necessary but not sufficient.
anchors:
  - path: source/toolkit/internal/curator/curator.go
    blob: 1e7fb72d46d4
confidence: verified
---

`allowedTools` builds grants scoped to the store's directories —
`"Bash(rm "+candidatesDir+"/*)"` (`source/toolkit/internal/curator/curator.go:268`) and its notes-directory
twin (`:276`), deliberately scoped rather than a bare `rm` so the one agent holding a
constructed grant cannot remove anything else (see
[[curator-write-grant-is-spelled-edit-with-no-mode]]).

**The allowlist is not the only gate.** The provider applies its own working-directory guard to
Bash file operations, and that guard can refuse a path the grant permits *and* that lies inside
the working directory it names as allowed. Reproduced in the worktree
`.../agentic-toolkit/chore/stage-handoff-memory`, where a single `touch` refused with:

    touch in '<worktree>/.agents/memory/candidates/.rmprobe' was blocked. For security,
    Claude Code may only create or modify files in the allowed working directories for this
    session: '<worktree>'

Three things narrow it, each checked in the same session:

- It is **not specific to the memory store** — `touch <worktree>/docs/.rmprobe-docs` refused
  identically.
- It is **not a symlink or realpath mismatch** — no component of the path is a link.
- It is **Bash-only** — the Write tool created files under `<worktree>/.agents/memory/notes/`
  in that same session without complaint, so the guard sits on Bash rather than on the path.

The cause was not established.

What did **not** reproduce: an earlier session reported the same guard refusing `rm` under the
staging directory ("may only remove files from the allowed working directories for this
session"), leaving a curation run's backlog uncleared. Here `rm` on the same directory
succeeded while `touch` was refused, so the guard's scope is narrower than that report — take
"deletion is refused" as unconfirmed. The transferable part is the shape: **a constructed
`Bash(...)` grant is necessary, not sufficient**, and a run can hold the grant and still be
stopped. If a curation run's backlog survives, check `notes/` and `agtk memory lint` before
concluding curation did not run, and clear the files from an ordinary shell.
