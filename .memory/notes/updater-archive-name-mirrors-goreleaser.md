---
name: updater-archive-name-mirrors-goreleaser
kind: invariant
description: source/toolkit/internal/updater reconstructs goreleaser's archive filename from scratch, and nothing checks the two still agree.
anchors:
  - path: source/toolkit/internal/updater/updater.go
    blob: 988145a4be2e
  - path: source/toolkit/internal/updater/updater_test.go
    blob: 2b81c32b734a
  - path: .goreleaser.yaml
    blob: d1a3c2e4d8f7
confidence: verified
---

Self-update works only while `GitHubInstaller.Install` and `.goreleaser.yaml` produce
byte-identical names. Three couplings, all implicit:

| updater.go | .goreleaser.yaml |
|---|---|
| `fmt.Sprintf("%s_%s_%s_%s.tar.gz", binary, strings.TrimPrefix(version, "v"), goos, goarch)` (`updater.go:88`) | `name_template: "agtk_{{ .Version }}_{{ .Os }}_{{ .Arch }}"` (`:30`) + `formats: [tar.gz]` (`:31`) |
| `binary` defaults to `"agtk"` (`updater.go:83-86`), used for the filename and for the tar entry lookup (`updater.go:112`) | `binary: agtk` (`:11`) |
| `checksumsURL = base + "/checksums.txt"` (`updater.go:92`) | `checksum: name_template: "checksums.txt"` (`:36-37`) |

The `TrimPrefix(version, "v")` at `updater.go:88` is load-bearing: goreleaser's `.Version` is
the tag *without* the leading `v`, while the release-download path segment
(`updater.go:89-90`) uses the tag *with* it. Both appear in the same URL, spelled differently
on purpose.

What breaks: editing `name_template`, `binary:`, or the archive format breaks `agtk update`
for **every already-installed binary in the field**, and only after the release is published
— the old binaries construct a URL that 404s. The new binary being built at the time is fine,
so a local build, the test suite and CI all stay green.

Nothing guards this. `source/toolkit/internal/updater/updater_test.go` exercises `lookupChecksum` and
`extractTarGz` with hand-written literals (`agtk_1.0.0_darwin_arm64.tar.gz`,
`updater_test.go:12`) that happen to match today's template but are never derived from it;
`Install` itself is never called in a test, and the CLI tests inject a stub `Installer`.
`grep -rn goreleaser` finds no test or CI step that cross-checks the two.

Related: `.goreleaser.yaml:18-20` builds only `darwin` and `linux`, while `Install` will
happily build a `windows` archive name from `runtime.GOOS`.

A fourth coupling arrived with the module move: the build now needs `dir: source/toolkit`
(`.goreleaser.yaml:14`) plus `main: ./cmd/agtk` relative to it (`:15`), while `archives.files`
(`:32-34`) and the `before` hook `go -C source/toolkit mod tidy` (`:7`) stay relative to the
repo root. That asymmetry is inside one file, so it is at least visible in a diff — unlike the
three above.
