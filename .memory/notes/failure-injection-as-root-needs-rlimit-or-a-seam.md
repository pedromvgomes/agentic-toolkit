---
name: failure-injection-as-root-needs-rlimit-or-a-seam
kind: gotcha
description: Tests run as root, so permission bits cannot force an I/O failure; use RLIMIT_FSIZE or a package-level seam.
anchors:
  - path: source/toolkit/internal/memory/tests/hits_write_failure_linux_test.go
    blob: c26812467695
  - path: source/toolkit/internal/memory/hits.go
    blob: 55841d9cd15f
  - path: source/toolkit/internal/memory/hits_compact_remove_test.go
    blob: beadd357c45a
confidence: verified
---

A read-only directory or a `chmod 0` file does not make a write or a remove fail in the cloud
container this repo is developed in, because the test process is root. Two seams work instead.

- **File size limit.** `underFileSizeLimit` (`memory/tests/hits_write_failure_linux_test.go:20`)
  lowers `RLIMIT_FSIZE`, so a write past the limit fails with `EFBIG` even as root. The Go runtime
  ignores the `SIGXFSZ` that goes with it. The limit applies to every file the process writes while
  the callback runs, so the callback must not log, and the tests in the package must not run in
  parallel. The helper skips when the hard limit is below the requested one.
- **Package-level variable.** `removeShard` (`memory/hits.go:368`) is `os.Remove` behind a
  variable so an in-package test (`memory/hits_compact_remove_test.go`, package `memory`, not
  `tests`) can refuse a removal. No file on disk forces `os.Remove` to fail for root.

The `_linux_test.go` suffix restricts the rlimit file to Linux by filename, and the file reads
`/proc/self/fd` to prove no descriptor leaks after a failed write.
