---
name: auto-update-config-does-not-gate-agtk-update
kind: gotcha
description: auto_update config gates only the background checker; `agtk update` never reads it, despite the doc comment saying it does.
anchors:
  - path: source/toolkit/internal/userconfig/types.go
    blob: f33029382876
  - path: source/toolkit/internal/cli/update.go
    blob: 8d3c6530b226
  - path: source/toolkit/internal/cli/root.go
    blob: 4eb683110c5a
confidence: verified
---

The doc comment on `AutoUpdate` says it "gates the background update-check goroutine **and
informs `agtk update`**" (`source/toolkit/internal/userconfig/types.go:23-24`). The second half is false.

`source/toolkit/internal/cli/update.go` neither imports nor reads `userconfig`.
`grep -rn userconfig --include='*.go' .` outside `source/toolkit/internal/userconfig/` returns three
production sites — `source/toolkit/internal/cli/root.go:26,187`, `source/toolkit/internal/updatecheck/throttle.go:8,23` and
`source/toolkit/internal/githubapp/credential.go:22,61` (plus one test). Only the first reaches
`userconfig.Load`; `credential.go` re-exports `userconfig.Dir()` and reads no config. `runUpdate`
(`source/toolkit/internal/cli/update.go:67`) goes straight from `version.IsDev()` (`:69`) to constructing a
provider (`:74-79`).

So `auto_update.enabled: false` and `check_interval` affect **only** the implicit background
check spawned from `PersistentPreRunE` (`source/toolkit/internal/cli/root.go:182-211`, itself skipped for
`cmd.Name() == "update"`). The explicit `agtk update` still hits the network, still offers to
install, and ignores both fields.

What breaks: a user in an air-gapped or locked-down environment sets `enabled: false`
believing it stops all outbound release traffic, and gets a network call the moment anyone
types `agtk update`. The only thing that stops `agtk update` is `version.IsDev()`. If the
config is meant to gate both, `runUpdate` is what has to change — `ShouldCheck`
(`source/toolkit/internal/updatecheck/throttle.go:36`) is called from `startBackgroundCheck` only.

See also [[user-config-errors-are-swallowed-by-root]], the other place this package's doc
comments describe behaviour the code does not have.
