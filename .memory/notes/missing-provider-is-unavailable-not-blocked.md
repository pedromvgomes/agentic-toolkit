---
name: missing-provider-is-unavailable-not-blocked
kind: gotcha
description: A review provider missing from PATH or failing at exec is an ordinary Unavailable report, never Blocked, so it does not trigger the panel's declared fallback.
anchors:
  - path: source/toolkit/internal/reviewrun/invoke.go
    blob: ae55e9629092
  - path: source/toolkit/internal/review/capability.go
    blob: b4a67453292c
  - path: source/toolkit/internal/provider/provider.go
    blob: 0c21a603ff46
  - path: docs/adr/0015-a-block-tries-the-other-provider-and-never-posts.md
    blob: a361b3b6bdaa
confidence: verified
---

`codex: executable file not found in $PATH` is expected on a machine without the codex CLI. The
providers are resolved from PATH, and the lookup happens lazily in the driver per run, not in
`review.CheckCapabilities` (`review/capability.go`), which checks only the declared capabilities
against the provider's Go type, never whether its binary exists.

`classify` (`source/toolkit/internal/reviewrun/invoke.go:137`) maps any `err != nil` from
`Invoke` (LookPath failure, fork/exec failure) to `Unavailable`; only `res.Blocked != nil`
(`:139`, a quota/credential decline) becomes `Blocked`. The panel `fallback:` fires only on
Blocked (ADR 0015 says a bad schema, sandbox refusal or timeout "is never blocked"), so a missing
provider never degrades to the other one.

The cross-model property described in `review/default.yaml`'s header is roster design intent,
not an enforced invariant; nothing refuses a single-provider manifest.
