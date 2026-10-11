---
name: cloudinit-is-environment-surface-not-credential-surface
kind: gotcha
description: "internal/cloudinit is in environmentSurface but deliberately not credentialSurface: that surface bans os.OpenFile and the key write needs one at 0600, so a separate test requires the write to exist and carry 0o600."
anchors:
  - path: source/toolkit/internal/cli/tests/credential_surface_test.go
    blob: f70907d7d85a
  - path: source/toolkit/internal/cloudinit/signing.go
    blob: d5da6d161b7f
confidence: verified
---

`TestNoInstallationTokenIsWrittenAnywhere` bans `os.WriteFile`, `os.Create` and `os.OpenFile` in the
non-test files of `credentialSurface` (`credential_surface_test.go:26`). `writeAtomic` must `OpenFile`
the signing key (`cloudinit/signing.go:119`), so adding the package there fails that test.

`cloudinit` joins `environmentSurface` only (`credential_surface_test.go:37-41`), and
`TestCloudInitWritesTheSigningKeyOwnerOnly` (`:231`) has the opposite shape: it requires at least one
`os.OpenFile`, requires every `OpenFile`/`WriteFile` mode to be the literal `0o600` (a named constant
counts as an offender), and refuses `os.Create`. See [[credential-guards-are-hand-maintained-lists]].
