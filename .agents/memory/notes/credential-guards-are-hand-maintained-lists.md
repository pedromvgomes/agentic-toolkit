---
name: credential-guards-are-hand-maintained-lists
kind: gotcha
description: Half the credential-surface guards are scoped to package lists a person edits, so a new package is not covered until someone adds it by name.
anchors:
  - path: internal/cli/tests/credential_surface_test.go
    blob: f0028924f45d
confidence: verified
---

`internal/cli/tests/credential_surface_test.go` carries the guards that make ADR 0006's
"no credential reaches a model's process" a property rather than a sentence. Three of them
are list-scoped, and none of the lists is derived from imports, a marker, or anything else —
they are plain Go slices someone maintains:

- `credentialSurface` (`:22`) — `internal/githubapp`, `internal/reviewpost`,
  `internal/reviewapprove`. Walked whole by `TestTheCredentialIsNeverPutIntoTheProcessEnvironment`
  (`:57`, bans `os.Setenv`/`os.Environ`) and `TestNoInstallationTokenIsWrittenAnywhere`
  (`:112`, bans `os.WriteFile`/`os.Create`/`os.OpenFile` outside `registrationWriter`, the one
  file allowed to persist a registration).
- the literal slice in `TestTheModelInvokingPackagesCannotReachTheCredential` (`:33-37`) —
  `reviewrun`, `curator`, `review`, checked with `go list -deps` against `credentialPackage`
  (`:17`).
- the literal slice in `TestNoReviewPathCanReachTheApproval` (`:199-204`) — same four-package
  shape, checked against `internal/reviewapprove`.

Walking a package *whole* is deliberate, so files added to a listed package stay covered
(`:108-111`). A **new sibling package** is the hole: it is not walked, and nothing fails.
`internal/reviewapprove` had to be typed into `credentialSurface` by hand to be covered, and a
new model-invoking package would need typing into the other two slices or their property
silently stops being checked for it.

The three guards that are not list-scoped walk all of `internal/`:
`TestOnlyOnePackageNamesTheApprovalEvent` (`:159`, bans the literal `"APPROVE"` outside
`internal/reviewapprove`), `TestNothingInTheBinaryWritesToARepository` (`:244`, bans the
`writeEndpoints` path fragments at `:223`), and the import-graph checks above are the
load-bearing half of each pair. So: **when you add a package that touches GitHub, the
credential, or a model, add it to the lists in this file in the same commit.**

Related: [[review-is-model-free-reviewrun-is-not]].
