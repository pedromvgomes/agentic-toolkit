---
name: completion-refresh-follows-the-self-replace
kind: invariant
description: The completion refresh shells out to whatever binary is on disk, so it must run after the self-replace and its failure must stay non-fatal.
anchors:
  - path: source/toolkit/internal/completioninstall/install.go
    blob: 71b67e0cd008
  - path: source/toolkit/internal/cli/update.go
    blob: b2b0526bee75
confidence: verified
---

`completioninstall` does not generate the completion script itself. `defaultRun`
(`source/toolkit/internal/completioninstall/install.go:242`) runs `exec.Command(exe, "completion", shell)`
where `exe` defaults to `os.Executable()` (`install.go:137-140`). The output therefore
reflects whichever binary sits at that path *at the moment `Install` is called*.

Two things a reader can get wrong:

1. **Ordering.** `runUpdate` calls `installer.Install(info.Latest)` first
   (`source/toolkit/internal/cli/update.go:113`) and `completioninstall.Install` only afterwards (`:124`).
   Hoisting the refresh earlier, or running it alongside the download, emits the *old*
   command tree into the completion file and reports success — new subcommands are silently
   missing from completion until the next update.
2. **Non-fatal by construction.** The refresh's error is printed to stderr as a hint and
   discarded (`source/toolkit/internal/cli/update.go:124-127`); `runUpdate` still returns nil. The binary
   swap has already happened, so returning the error would make a completed, irreversible
   update exit non-zero and read as "the update failed". Anyone tightening error handling
   here has to preserve that: after the swap, nothing may turn the command into a failure.

`defaultRun` also carries `nosemgrep` and `#nosec G204 G702` directives on the call line
itself (`install.go:243-244`) because semgrep anchors to the finding line. Moving the exec
into a helper re-opens the finding.

Related: [[completion-paths-are-implemented-twice]].
