---
about: auto_update config gates only the background checker; `agtk update` never reads the user config despite the doc comment saying it does
saw:
  - internal/userconfig/types.go
  - internal/cli/update.go
  - internal/cli/root.go
---

Kind: **gotcha**.

The doc comment on `AutoUpdate` says it "gates the background update-check goroutine **and
informs `agtk update`**" (internal/userconfig/types.go:23-24). The second half is false.

Quantified claim, verified across the whole Go tree:
`grep -rn "userconfig" . --include="*.go"` outside `internal/userconfig/` returns exactly
four production sites — `internal/cli/root.go:26,187` and
`internal/updatecheck/throttle.go:8,23` (plus one test). `internal/cli/update.go` neither
imports nor reads `userconfig`; `runUpdate` (update.go:67-125) goes straight from
`version.IsDev()` to constructing a provider.

So `auto_update.enabled: false` and `check_interval` affect **only** the implicit
background check spawned from `PersistentPreRunE` (root.go:182-211, itself skipped for
`cmd.Name() == "update"`). The explicit `agtk update` still hits the network, still offers
to install, and ignores both fields entirely.

What breaks when someone gets this wrong: a reader wiring a new gate, or a user in an
air-gapped/locked-down environment who sets `enabled: false` believing it disables all
outbound release traffic, gets a network call anyway the moment anyone types `agtk update`.
The only thing that stops `agtk update` is `version.IsDev()` (update.go:69-72). If the
intent is for the config to gate both, `runUpdate` is the place that has to change; do not
assume `ShouldCheck` covers it — `ShouldCheck` is called from one place only.
