---
about: "ParseSession resolves a session's identity from the first non-empty Cwd in row order, so the main transcript's directory decides the repo and project for its subagent rows too, and a session that changes directory partway keeps its first one"
saw:
  - source/toolkit/internal/usage/session.go
  - source/toolkit/internal/usage/identity.go
  - source/toolkit/internal/usage/tests/usage_test.go
---

`sessionCwd` in `usage/session.go` returns the first non-empty `Row.Cwd` across the joined rows
(main transcript first, then subagent files sorted by name). The identity is resolved once from it
and the same value is stamped on every row, so a subagent that ran in a different directory does
not change its rows' repo. A subagent's cwd is used only when the main transcript yields none. An
empty cwd makes `repoOf` in `usage/identity.go` skip git entirely rather than run it in the
process's own directory. The resulting `*string` fields are shared by every row of the session.
