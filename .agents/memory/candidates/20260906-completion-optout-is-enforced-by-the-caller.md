---
about: completioninstall.Install never reads AGTK_NO_COMPLETION — the opt-out lives entirely in the caller
saw:
  - internal/completioninstall/install.go
  - internal/cli/update.go
---

Kind: **gotcha**.

`Options.Disabled`'s doc comment at `internal/completioninstall/install.go:44` reads
"used for AGTK_NO_COMPLETION and --no-completion", which invites the reading that the
package honours the environment variable. It does not. `Install` (`install.go:84`) only
checks `opts.Disabled`; the environment read is at the single call site,
`internal/cli/update.go:123`:

```go
disabled := noCompletion || os.Getenv("AGTK_NO_COMPLETION") == "1"
```

`grep -rn AGTK_NO_COMPLETION --include='*.go' .` -> three hits in `update.go` (flag help,
long help, that line) and the doc comment above. Nothing in the package.

The package *does* read the environment for the two other ambient inputs — `$SHELL`
(`install.go:96`) and `$HOME` (`install.go:107`) — which makes the asymmetry easy to miss.

What breaks: a second caller that passes `Options{}` (or forgets the `os.Getenv`) writes a
completion script into the user's home directory even though they set `AGTK_NO_COMPLETION=1`.
There is no test covering that path — `TestInstall_Disabled` sets the field directly.
