---
about: curate refuses a real (non-dry) run on a provider that cannot express a delegation deny list, mirroring how confine() already gates the tool allow-list
saw:
  - source/toolkit/internal/curator/curator.go
  - source/toolkit/internal/curator/tests/codex_test.go
---

`disallow()` in `curator.go` asks the provider whether it implements
`agentic.Disallower` (agentic-driver's optional interface for
`--disallowedTools`-shaped denial). claudecode implements it; codex does not
— it has no per-tool vocabulary at all, only a sandbox mode.

For a provider without the capability, a real curate run refuses outright
before starting the child process, exactly the way `confine()` already
refuses a real run on a provider with no `AllowedTools` vocabulary. A dry
run is exempt (it grants no writing tools regardless, so there is nothing
delegation could touch), which is why
`TestADryRunIsBoundedBySandboxWhenThereIsNoAllowlist` and
`TestThePolicyIsInlinedForAProviderThatCannotDefineAgents` in
`codex_test.go` still pass against codex.

The reason to refuse rather than silently proceed without the deny list:
the whole point of `DisallowedTools` here is closing off the curator's
ability to delegate to a subagent (the Agent/Task tools, which need no
permission grant to invoke and whose background work can be silently
dropped by the CLI's own idle-wait ceiling — this is what let a cut-off
curator run report exit 0 with the store half-curated). A provider that
cannot honor the deny list is a provider this guarantee cannot be made for,
so `Check`/`Run` treat "cannot deny" the same as "cannot confine": refuse,
don't degrade.
