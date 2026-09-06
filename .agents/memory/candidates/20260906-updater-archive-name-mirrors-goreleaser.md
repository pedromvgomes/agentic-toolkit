---
about: internal/updater reconstructs goreleaser's archive filename from scratch, and nothing checks the two still agree
saw:
  - internal/updater/updater.go
  - internal/updater/updater_test.go
  - .goreleaser.yaml
---

Kind: **invariant**.

Self-update works only while `GitHubInstaller.Install` and `.goreleaser.yaml` produce byte-identical
names. Three couplings, all implicit:

| updater.go | .goreleaser.yaml |
|---|---|
| `fmt.Sprintf("%s_%s_%s_%s.tar.gz", binary, strings.TrimPrefix(version, "v"), goos, goarch)` (updater.go:88) | `name_template: "agtk_{{ .Version }}_{{ .Os }}_{{ .Arch }}"` + `formats: [tar.gz]` |
| `binary` defaults to `"agtk"` (updater.go:83-86), used both for the filename and for the tar entry lookup (updater.go:112) | `builds: binary: agtk` |
| `checksumsURL = base + "/checksums.txt"` (updater.go:92) | `checksum: name_template: "checksums.txt"` |

The `TrimPrefix(version, "v")` at updater.go:88 is load-bearing: goreleaser's `.Version` is
the tag *without* the leading `v`, while the release-download path segment
(updater.go:89-90) uses the tag *with* it. Both appear in the same URL, spelled differently
on purpose.

What breaks if someone gets it wrong: editing `name_template`, `binary:`, or the archive
format breaks `agtk update` for **every already-installed binary in the field**, and does so
only after the release is published — the old binaries construct a URL that 404s. The new
binary being built at the time is fine, so a local build, the test suite and CI all stay
green.

Nothing guards this. `internal/updater/updater_test.go` exercises `lookupChecksum` and
`extractTarGz` with hand-written literals (`agtk_1.0.0_darwin_arm64.tar.gz`,
updater_test.go:12) that happen to match today's template but are never derived from it;
`Install` itself is never called in a test, and the CLI tests inject a stub `Installer`
(the seam described at updater.go:14-16). `grep -rn goreleaser` finds no test or CI step
that cross-checks the two.

Related: `.goreleaser.yaml` builds only `darwin` and `linux`, while `Install` will happily
build a `windows` archive name from `runtime.GOOS`.
