---
about: "internal/cloudinit is in environmentSurface but deliberately not credentialSurface: the surface bans os.OpenFile and the key write needs one at 0600, so a separate test requires the write to exist and carry 0o600"
saw:
  - source/toolkit/internal/cli/tests/credential_surface_test.go
  - source/toolkit/internal/cloudinit/signing.go
---

`TestNoInstallationTokenIsWrittenAnywhere` bans `os.WriteFile`, `os.Create` and `os.OpenFile` in every
non-test file of `credentialSurface`. `writeAtomic` in `cloudinit/signing.go` has to `OpenFile` the
signing key, so adding the package to `credentialSurface` fails that test.

The package joins `environmentSurface` only (no `os.Setenv`/`os.Environ`), and
`TestCloudInitWritesTheSigningKeyOwnerOnly` has the opposite shape: it parses the package, requires at
least one `os.OpenFile`, requires every `OpenFile`/`WriteFile` mode to be the literal `0o600`, and
refuses `os.Create`. A mode held in a named constant counts as an offender, so the literal stays at
the call site. Changing the mode in `signing.go` to `0o644` makes the test fail.

Both lists are hand-maintained, so a new package that handles a secret is added by name.
