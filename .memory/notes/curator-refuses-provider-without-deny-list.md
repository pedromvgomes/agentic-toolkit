---
name: curator-refuses-provider-without-deny-list
kind: invariant
description: A real curate run is refused on a provider that cannot express a tool deny list, because the deny list is what stops the curator delegating to a subagent.
anchors:
  - path: source/toolkit/internal/curator/curator.go
    blob: b7d9970d93f2
  - path: source/toolkit/internal/curator/tests/codex_test.go
    blob: ba801b19c1f7
confidence: verified
---

`disallow()` (`source/toolkit/internal/curator/curator.go:465`) type-asserts the provider to
`agentic.Disallower`. claudecode implements it; codex has no per-tool vocabulary, only a sandbox
mode. Without it a non-dry run returns an error before any child starts, the same way
`confine()` (`:426`) refuses a provider with no allowed-tools vocabulary. Dry runs are exempt
(no writing tools are granted), which is why the codex tests in
`tests/codex_test.go` still pass.

Why refuse rather than degrade: the deny list closes off the Agent/Task tools, which need no
permission grant and whose background work can be dropped by the CLI's idle-wait ceiling. That
let a cut-off curator report exit 0 with the store half-curated (see
[[curate-verification-is-bidirectional]]). A provider that cannot deny cannot give the
guarantee, so "cannot deny" is treated as "cannot confine".
