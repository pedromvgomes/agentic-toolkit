---
about: an empty or comment-only user config.yaml is a hard parse error, and the only caller swallows it — auto-update goes silently off
saw:
  - internal/userconfig/loader.go
  - internal/userconfig/types.go
  - internal/cli/root.go
---

Kind: **gotcha**.

`LoadFrom` (internal/userconfig/loader.go:52-55) decodes with
`yaml.NewDecoder(..., yaml.Strict())`. On a file with no YAML document — empty, whitespace,
or comments only — `Decode` returns `io.EOF`, which is wrapped and returned as an error.
Only a file with at least one real node (e.g. `auto_update:` with nothing under it) parses.

Probed directly against `userconfig.LoadFrom` with four bodies:

```
contents=""                  -> err = userconfig: parse …/config.yaml: EOF
contents="\n"                -> err = userconfig: parse …/config.yaml: EOF
contents="# just a comment\n"-> err = userconfig: parse …/config.yaml: EOF
contents="auto_update:\n"    -> cfg = {Enabled:true CheckInterval:24h}, err = nil
```

Note the asymmetry: a **missing** file returns `Default()` with no error
(loader.go:45-47), but a **present but empty** file is an error. `touch config.yaml` is
therefore not equivalent to having no config at all.

What breaks: the sole production caller is `startBackgroundCheck`
(internal/cli/root.go:187-190), which does `if err != nil { return nil }` — auto-update is
skipped with no message on stdout or stderr. So a user who creates the config file and
leaves it empty (or comments out every line while debugging) turns the update checker off
permanently and gets no signal. The same swallow hides every other config error: a typo'd
key is rejected by `yaml.Strict()` (proved by
internal/userconfig/tests/loader_test.go:43-53) and then discarded by root.go.

This directly contradicts the package doc at internal/userconfig/types.go:11-13 — "Unknown
keys are rejected so misspellings surface immediately rather than silently disabling
features." The rejection happens; the surfacing does not. Anyone reading only the package
doc will believe a malformed config is loud. It is silent.
