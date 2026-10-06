---
about: "ssh-keygen -y and -Y sign prompt for a passphrase on /dev/tty; cloud init runs each in a new session with stdin closed and a context timeout, so a passphrase-protected key fails fast instead of hanging a session start"
saw:
  - source/toolkit/internal/cloudinit/signing.go
  - source/toolkit/internal/cloudinit/tests/cloudinit_test.go
---

`sshKeygen` in `signing.go` sets `SysProcAttr{Setsid: true}` and leaves `Stdin` nil (so /dev/null).
With no controlling terminal there is nothing for ssh-keygen to prompt on, a passphrase read gets end
of file, and the command fails; the context timeout covers any prompt that would still wait. The
stderr that comes back is checked for the word "passphrase" to give the clear "holds a
passphrase-protected key" error.

`TestAPassphraseProtectedKeyFailsClearlyWithoutHanging` generates a passphrase-protected key and
finishes in a fraction of a second. `syscall.SysProcAttr.Setsid` is Unix-only, so the package does
not compile for Windows; goreleaser builds only darwin and linux.

Error text is scrubbed: any ssh-keygen message containing the encoded key or a line of the decoded
one is replaced wholesale, because redacting in place would leave the rest of a quoted key.
