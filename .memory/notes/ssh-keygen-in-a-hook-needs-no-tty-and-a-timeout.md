---
name: ssh-keygen-in-a-hook-needs-no-tty-and-a-timeout
kind: gotcha
description: ssh-keygen prompts for a passphrase on /dev/tty; cloud init runs it in a new session with stdin closed and a context timeout, so a passphrase-protected key fails fast instead of hanging a session start.
anchors:
  - path: source/toolkit/internal/cloudinit/signing.go
    blob: d5da6d161b7f
  - path: source/toolkit/internal/cloudinit/tests/cloudinit_test.go
    blob: d32e70f48310
confidence: verified
---

`sshKeygen` sets `cmd.Stdin = nil` and `SysProcAttr{Setsid: true}` (`cloudinit/signing.go:175-176`) so
there is no controlling terminal to prompt on; the passphrase read hits end of file and the command
fails. A context timeout (`signing.go:172`) covers any prompt that would still wait. Stderr containing
"passphrase" yields the clear "holds a passphrase-protected key" error (`signing.go:196-197`).

`TestAPassphraseProtectedKeyFailsClearlyWithoutHanging` (`tests/cloudinit_test.go:262`) covers it.
`Setsid` is Unix-only, so the package does not compile for Windows; goreleaser builds only darwin and
linux. Error text containing the key or a line of it is replaced wholesale, not redacted in place.
