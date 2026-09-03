---
name: test-suite-time-is-dominated-by-real-git
kind: gotcha
description: sourcestore's tests shell out to real git and account for roughly four fifths of the suite's wall time.
anchors:
  - path: internal/sourcestore/tests/*.go
    matches:
      - path: internal/sourcestore/tests/annotated_tag_test.go
        blob: 96e48bfc84dd
      - path: internal/sourcestore/tests/frozen_provider_test.go
        blob: e8f60562036f
      - path: internal/sourcestore/tests/helpers_test.go
        blob: 3d68486cde48
      - path: internal/sourcestore/tests/live_provider_test.go
        blob: 65a2ccbc182d
confidence: verified
---

`make check` runs about a minute, of which `internal/sourcestore/tests` is roughly 48
seconds: those tests build fixture repositories on disk and drive real `git` subprocesses
(`fixtureRepoFromDir` in that package's helpers). Nothing there is slow by mistake — the
provider's whole job is git behaviour, and stubbing it would test nothing.

The practical consequence is for iteration, not for CI: `go test ./internal/memory/...`
finishes in under a second, so run the package you are editing and keep `make check` for
the end. Reaching for `-short` will not help; there are no `testing.Short` guards.
