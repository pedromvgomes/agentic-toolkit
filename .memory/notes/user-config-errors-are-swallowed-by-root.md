---
name: user-config-errors-are-swallowed-by-root
kind: gotcha
description: userconfig.Load's only caller discards the error, so a misspelled key silently disables auto-update — the outcome the package doc promises is impossible.
anchors:
  - path: source/toolkit/internal/cli/root.go
    blob: c38cddfed6d5
  - path: source/toolkit/internal/userconfig/loader.go
    blob: f0d8d9e6cc1b
  - path: source/toolkit/internal/userconfig/types.go
    blob: f33029382876
confidence: verified
---

`userconfig.Load` has one production caller, `startBackgroundCheck`
(`source/toolkit/internal/cli/root.go:187`), which throws the error away:

```go
cfg, err := userconfig.Load()
if err != nil {
    // Misconfigured user file: don't crash, just skip.
    return nil
}
```

Returning nil is also how every *deliberate* gate in that function says "no check"
(`source/toolkit/internal/cli/root.go:184-186`, `:200-202`), so a config that fails to parse is
indistinguishable from auto-update being switched off on purpose. Nothing is printed to
stdout or stderr on the way past.

The parse side is strict: `LoadFrom` decodes with `yaml.Strict()`
(`source/toolkit/internal/userconfig/loader.go:70`), so an unknown or misspelled key is a hard error, pinned
by `TestLoadFrom_RejectsUnknownKeys` (`source/toolkit/internal/userconfig/tests/loader_test.go:43-53`).

Put together, the package doc at `source/toolkit/internal/userconfig/types.go:11-13` — "Unknown keys are
rejected so misspellings surface immediately rather than silently disabling features" — is
wrong about the outcome it cares about most. The rejection happens; the surfacing does not. A
user who writes `auto_updates:` gets no error, no warning, and no update checks.

Scope: this is the error path only. The empty-file case is handled — `LoadFrom` returns
`Default(), nil` on `io.EOF` (`source/toolkit/internal/userconfig/loader.go:78-79`), so a `touch`ed or
comment-only `config.yaml` yields the defaults and never reaches the swallow. A fix lands
either in `root.go` (report before skipping) or in `types.go` (correct the doc).

The same doc block oversells the config's reach in a second way: see
[[auto-update-config-does-not-gate-agtk-update]].
