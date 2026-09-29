---
about: "curator-write-grant note's claims hold but every curator.go pointer has moved"
saw:
  - source/toolkit/internal/curator/curator.go
  - source/toolkit/internal/curator/prompt.md
  - docs/adr/0004-the-curator-ships-in-the-binary.md
targets: curator-write-grant-is-spelled-edit-with-no-mode
verdict: still-true
---

Re-read `curator.go` at HEAD. Grant construction and permission-mode reasoning are unchanged.
Corrected pointers: `allowedTools` `:290` (note `:216`); Edit grant `:322-324` (`:248-249`);
Edit/Write comment `:312-316` (`:238-241`); `editPattern` `:392` (`:318`); `permissionMode = ""`
`:490` with comment `:478-489` (`:398`, `:386-397`); candidate `rm` grant `:341-343` (`:267-268`);
note `rm` grant `:349-351` (`:275-276`); `anchorGrants` `:374` (`:300`). ADR 0004's argv-flag
sentence is at `docs/adr/0004-the-curator-ships-in-the-binary.md:43` (note says `:42`).
The same shifts apply to `scoped-anchor-grant-names-each-note-exactly` (`anchorGrants` `:374`,
rationale comment `:361-372`, not `:300`/`:288-299`; `nameRe` is still `memory/lint.go:14`).
