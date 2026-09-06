---
about: the completion refresh shells out to whatever binary is on disk, so it is only correct after the self-replace, and its failure must stay non-fatal
saw:
  - internal/completioninstall/install.go
  - internal/cli/update.go
---

Kind: **invariant**.

`completioninstall` does not generate the completion script itself. `defaultRun`
(`internal/completioninstall/install.go:232`) runs `exec.Command(exe, "completion", shell)`
where `exe` defaults to `os.Executable()` (`install.go:130`). The output therefore reflects
whichever binary sits at that path *at the moment Install is called*.

Two consequences a reader can get wrong:

1. **Ordering.** `runUpdate` calls `installer.Install(info.Latest)` first and
   `completioninstall.Install` only afterwards (`internal/cli/update.go:113` then `:124`).
   Hoisting the refresh earlier, or running it in parallel with the download, emits the
   *old* command tree into the completion file and reports success — new subcommands are
   silently missing from completion until the next update.
2. **Non-fatal by construction.** The refresh's error is printed to stderr as a hint and
   discarded (`update.go:125-128`); `runUpdate` still returns nil. The binary swap has
   already happened at that point, so returning the error would make a completed,
   irreversible update exit non-zero and read as "the update failed". Anyone tightening the
   error handling here has to preserve that: after the swap, nothing may turn the command
   into a failure.

`defaultRun` also carries `nosemgrep` and `#nosec G204 G702` directives on the call line
itself (`install.go:228-234`) — the comment above explains they must sit on the call, not
the declaration, because semgrep anchors to the finding line. Moving the exec into a helper
re-opens the finding.
