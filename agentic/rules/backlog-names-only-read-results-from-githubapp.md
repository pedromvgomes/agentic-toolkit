---
description: internal/backlog may name only read results and read errors from githubapp, and imports no other package of this module.
---

# The backlog package names only read results from githubapp

`internal/backlog` decides whether an unattended session may act on an issue, and it reads with a
caller's token only (ADR 0028). The Factory App's key and the review App's installation token must
never be reachable from it. It has to import `githubapp` for the types its reads return, so the
import graph cannot keep the credential out; the allow-list of names does.

## Applies to

Every non-test file under `source/toolkit/internal/backlog`. It may name `Issue`, `Permission`,
`IssueTrust`, `Actor`, `BodyEdit`, `LabelEvent`, `ErrNotFound`, `ErrForbidden` and
`ErrGraphQLUnavailable`, and imports no other package of this module.

## Example

Bad: calling `githubapp.Load` or naming `githubapp.Client` in `backlog` to read something the
`Reader` interface lacks. Add the read to `githubapp.ReadClient` instead.
`TestTheBacklogNamesNothingFromTheCredentialPackageButReadResults` in
`source/toolkit/internal/cli/tests/credential_surface_test.go` fails if the list is exceeded.
