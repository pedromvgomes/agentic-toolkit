---
name: auto-update-config-does-not-gate-agtk-update
kind: gotcha
description: auto_update config gates only the background checker; `agtk update` never reads it, despite the doc comment saying it does.
anchors:
  - path: internal/userconfig/types.go
    blob: f33029382876
  - path: internal/cli/update.go
    blob: b2b0526bee75
  - path: internal/cli/root.go
    blob: 87870af8e371
confidence: verified
---

The doc comment on `AutoUpdate` says it "gates the background update-check goroutine **and
informs `agtk update`**" (`internal/userconfig/types.go:23-24`). The second half is false.

`internal/cli/update.go` neither imports nor reads `userconfig`.
`grep -rn userconfig --include='*.go' .` outside `internal/userconfig/` returns exactly two
production sites — `internal/cli/root.go:26,187` and `internal/updatecheck/throttle.go:8,23`
(plus one test). `runUpdate` goes straight from `version.IsDev()` to constructing a provider.

So `auto_update.enabled: false` and `check_interval` affect **only** the implicit background
check spawned from `PersistentPreRunE` (`internal/cli/root.go:182-211`, itself skipped for
`cmd.Name() == "update"`). The explicit `agtk update` still hits the network, still offers to
install, and ignores both fields.

What breaks: a user in an air-gapped or locked-down environment sets `enabled: false`
believing it stops all outbound release traffic, and gets a network call the moment anyone
types `agtk update`. The only thing that stops `agtk update` is `version.IsDev()`. If the
config is meant to gate both, `runUpdate` is what has to change — `ShouldCheck`
(`internal/updatecheck/throttle.go:50`) is called from `startBackgroundCheck` only.

See also [[user-config-errors-are-swallowed-by-root]], the other place this package's doc
comments describe behaviour the code does not have.
