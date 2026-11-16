---
about: "GitHub's collaborator permission field folds maintain into \"write\" and triage into \"read\", so backlog's write-access check reads role_name too: only admin/write count, and a role_name of triage, read or none refuses even under them"
saw:
  - source/toolkit/internal/githubapp/issues.go
  - source/toolkit/internal/backlog/verify.go
  - docs/adr/0028-a-backlog-item-is-trusted-by-who-wrote-it.md
---

`githubapp.Permission` carries both `Permission` (coarse: admin/write/read/none) and `RoleName`
(as granted, including maintain, triage and custom roles). `grantsWrite` in `backlog/verify.go`
requires `Permission` to be admin or write and refuses a `RoleName` of triage, read or none.
`access.write` looks up only actors of type `User`: a bot's login is its App slug, and the
collaborator endpoint would read it as a same-named user. A bot counts only as the configured
`--trusted-app` (`appliedByTrustedApp`, matched as Type Bot with or without `[bot]`).
