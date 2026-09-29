---
about: internal/relay.Dispatch always targets ref "main" rather than asking GitHub which branch is the relay repository's default, so a relay repository whose default branch has any other name has every dispatch refused
saw:
  - source/toolkit/internal/relay/relay.go
  - CONTEXT.md
  - docs/adr/0020-a-relay-posts-as-the-app-from-a-separate-repository.md
---

`ref` in `relay.go` is the hardcoded constant `"main"`, sent as the `ref` field of every
`workflow_dispatch` POST. This was a deliberate trade rather than an oversight (recorded in the
`ref` constant's own comment and ADR 0020's consequences): reading the relay repository's actual
default branch first (`GET /repos/{slug}`) would cost one extra request on every dispatch, so the
constant instead requires the operator to keep the relay repository's default branch named `main`.

A relay repository created with a different default (GitHub still defaults new repositories to
`main`, but an imported or renamed one can carry any name) gets GitHub's own "No ref found" error
on every dispatch, with nothing in this repository's own code pointing at the cause — the fix is
entirely on the relay repository's side (rename its default branch, or move the workflow onto a
branch literally called `main`), not anything `internal/relay` can detect or work around.
