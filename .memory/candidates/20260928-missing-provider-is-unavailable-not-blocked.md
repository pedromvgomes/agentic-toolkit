---
about: a review provider that is missing from PATH or fails at exec surfaces as an ordinary Unavailable report, and does not trigger a panel's declared fallback to another provider
saw:
  - source/toolkit/internal/provider/provider.go
  - source/toolkit/internal/reviewrun/invoke.go
  - source/toolkit/internal/review/capability.go
  - source/toolkit/internal/review/default.yaml
  - docs/adr/0015-a-block-tries-the-other-provider-and-never-posts.md
---

`codex: executable file not found in $PATH` is expected behaviour, not a bug,
on any machine without the codex CLI installed. `provider.New()` always
resolves both providers via `NewOnPath()` (`source/toolkit/internal/provider/provider.go:53,55`)
— "Both are taken from PATH rather than vendored: agtk runs on a developer's
machine against the CLI they are already authenticated with, not against a
build this repo would have to pin." `NewOnPath()` does not touch PATH itself;
the actual `exec.LookPath` happens inside the driver's `resolveBinary()`,
invoked lazily per run by `driverInvoker.build` (`reviewrun/invoke.go:43-56`),
not by `review.CheckCapabilities` (`review/capability.go`), which only checks
the manifest's *declared* capabilities (schema support, confinement
vocabulary) against the provider's Go type — never whether its binary exists.

A missing/failing provider does **not** make a panel degrade to the other
provider automatically. `docs/adr/0015-a-block-tries-the-other-provider-and-never-posts.md`
documents the one exception: `agentic.Result.Blocked` (a quota/credential
decline). Both a fork/exec failure (E2BIG or otherwise) and a `LookPath` "not
found" surface as an ordinary `err != nil` from `Invoke`
(`reviewrun/invoke.go:135-138`, `classify`'s `case err != nil:` branch →
`Unavailable`, never `Blocked`), so neither triggers the panel's declared
`fallback:`. The ADR is explicit that this is deliberate: "a bad schema, a
sandbox refusal, a timeout — is never blocked and always posts, unchanged."

The built-in `quick` and `deep` panels (`review/default.yaml:36-53`) are both
all-claudecode, so any provider-level failure looks identical across them —
the panel choice never differs by provider until a `-codex` twin panel is
selected (`defaults.pr: quick-codex`, `default.yaml:84`) or a `Blocked`
fallback fires.

No ADR or note found requiring a panel to be multi-provider/cross-model in
general — the cross-model property (`review/default.yaml:7-10`, "so a change
is read by two models trained differently before anyone else sees it")
describes the roster's design intent, not an enforced invariant; nothing in
`review/manifest.go` or `capability.go` refuses a single-provider manifest.
