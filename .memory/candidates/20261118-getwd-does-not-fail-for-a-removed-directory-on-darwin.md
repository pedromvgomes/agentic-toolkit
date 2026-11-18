---
about: "os.Getwd still returns a path after the working directory is removed on darwin, so the cloud init resolve-the-working-directory failure can only be provoked on Linux"
saw:
  - source/toolkit/internal/cloudinit/git.go
  - source/toolkit/internal/cloudinit/tests/options_test.go
---

`workTree` returns "cloud init: resolve the working directory" when `os.Getwd` fails. On darwin Go
resolves the cwd through the descriptor, so removing the directory, or removing search permission
from its parent, still yields a path and the failure moves to a later `git` call. The test for the
branch skips on darwin and runs on the Linux CI host. A test that compares the fake executable's
`$PWD` to git's `--show-toplevel` has the same macOS split: the logical path is `/var/...` and git
reports `/private/var/...`, so the script logs `pwd -P`.

Established by probing `os.Getwd` after removal and after `chmod 0` of the parent on macOS.
