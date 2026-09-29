---
name: relay-dispatch-ref-is-hardcoded-main
kind: gotcha
description: internal/relay dispatches workflows on the literal ref "main", so a relay repository whose default branch has another name has every dispatch refused.
anchors:
  - path: source/toolkit/internal/relay/relay.go
    blob: 1254d81c9b7f
  - path: docs/adr/0020-a-relay-posts-as-the-app-from-a-separate-repository.md
    blob: dfa36e5e362f
confidence: verified
---

`ref = "main"` (`source/toolkit/internal/relay/relay.go:50`) is sent as the `ref` of every
`workflow_dispatch`. The comment above it records the trade: asking GitHub for the default
branch would cost one more request per dispatch, so the operator must keep the relay
repository's default branch named `main`. ADR 0020 lists it under consequences.

A relay repository with another default (imported or renamed) gets GitHub's "No ref found" on
every dispatch, and nothing in this repo points at the cause. The fix is on the relay side:
rename the branch or put the workflow on one called `main`.
