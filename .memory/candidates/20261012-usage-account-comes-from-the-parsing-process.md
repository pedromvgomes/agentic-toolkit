---
about: "A usage row's Claude account (account_uuid, organization_uuid, user_email) is read from CLAUDE_CODE_ACCOUNT_UUID, CLAUDE_CODE_ORGANIZATION_UUID and CLAUDE_CODE_USER_EMAIL in the environment of the process that runs ParseSession, never from the transcript, so parsing another account's transcript stamps the parser's account"
saw:
  - source/toolkit/internal/usage/identity.go
  - source/toolkit/internal/usage/session.go
  - source/toolkit/internal/usage/tests/usage_test.go
---

`ResolveIdentity` in `usage/identity.go` takes an injected `LookupEnv` and reads the three account
variables plus `CLAUDE_CODE_REMOTE` (host) through it; `ParseSession` in `usage/session.go` passes
`os.LookupEnv`. Transcript records carry `cwd` but no account, so nothing in the file can supply
one. The consequence is that `agtk` run under account A over account B's transcripts labels every
row A. The names were observed in a live cloud session's process environment, not in any Claude
Code documentation. `ParseFile` never reads the environment; only `ParseSession` stamps, which
`usage/tests/usage_test.go` pins by setting every variable and asserting `ParseFile` rows stay
unstamped.
