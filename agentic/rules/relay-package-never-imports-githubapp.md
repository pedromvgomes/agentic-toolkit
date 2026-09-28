---
description: internal/relay must never import internal/githubapp, keeping the package that carries a caller's token out of reach of the package that holds the App's key.
---

# `internal/relay` must never import `internal/githubapp`

The relay carries a caller's own token to a dedicated repository whose own workflow holds the
App's registration; the raw key never leaves that workflow's runner. Keeping `internal/relay`
out of reach of `internal/githubapp` makes that a property of the import graph, so handing the
relay the App's credential would take adding an import rather than forgetting to remove one (see
ADR 0020).

## Applies to

`source/toolkit/internal/relay`, and any future file added to that package.

## Example

Good: `internal/relay` declares its own `Doer`, `Target` and `Error` types and never references
`internal/githubapp`.

Bad: importing `internal/githubapp` into `internal/relay` for a helper or a shared type —
`TestTheRelayCannotReachTheCredential` in
`source/toolkit/internal/cli/tests/credential_surface_test.go` fails if it happens.
