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
    blob: 9b775bf0b883
confidence: verified
---

The doc comment on `AutoUpdate` says it "gates the background update-check goroutine **and
informs `agtk update`**" (`internal/userconfig/types.go:23-24`). The second half is false.

`internal/cli/update.go` neither imports nor reads `userconfig`.
`grep -rn userconfig --include='*.go' .` outside `internal/userconfig/` returns three
production sites — `internal/cli/root.go:26,187`, `internal/updatecheck/throttle.go:8,23` and
`internal/githubapp/credential.go:22,61` (plus one test). Only the first reaches
`userconfig.Load`; `credential.go` re-exports `userconfig.Dir()` and reads no config. `runUpdate`
(`internal/cli/update.go:67`) goes straight from `version.IsDev()` (`:69`) to constructing a
provider (`:74-79`).

So `auto_update.enabled: false` and `check_interval` affect **only** the implicit background
check spawned from `PersistentPreRunE` (`internal/cli/root.go:182-211`, itself skipped for
`cmd.Name() == "update"`). The explicit `agtk update` still hits the network, still offers to
install, and ignores both fields.

What breaks: a user in an air-gapped or locked-down environment sets `enabled: false`
believing it stops all outbound release traffic, and gets a network call the moment anyone
types `agtk update`. The only thing that stops `agtk update` is `version.IsDev()`. If the
config is meant to gate both, `runUpdate` is what has to change — `ShouldCheck`
(`internal/updatecheck/throttle.go:36`) is called from `startBackgroundCheck` only.

See also [[user-config-errors-are-swallowed-by-root]], the other place this package's doc
comments describe behaviour the code does not have.
