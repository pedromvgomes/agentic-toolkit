---
about: "cloudinit's --render runs os.Executable, which under go test is the test binary, so the render tests re-execute it as the real agtk through TestMain, and CLI tests must never point --render at a renderable checkout"
saw:
  - source/toolkit/internal/cloudinit/render.go
  - source/toolkit/internal/cloudinit/tests/main_test.go
  - source/toolkit/internal/cli/tests/cloud_init_test.go
---

`renderOne` in `cloudinit/render.go` runs `<os.Executable> render` in each checkout, so the render
comes from the same build as the command running it. Under `go test` that executable is the test
binary, not agtk. `cloudinit/tests/main_test.go` handles it: when `AGTK_CLOUDINIT_TEST_ACT_AS_AGTK`
is `1` in the child's environment, `TestMain` calls `cli.Execute()` instead of `m.Run()`, so a test
exercises the real `agtk render` without building agtk first.

The same re-execution is a trap in the CLI tests: `cli/tests` has no such `TestMain`, so a CLI test
that points `--render-root` at a checkout the command accepts would run the CLI test binary with
the argument `render`, which re-runs the whole suite. `cloud_init_test.go` therefore only uses an
empty root or the unconfigured path (see the comment near line 205 of `cloud_init_test.go`).
`Options.Executable` exists so a test can name a stub instead.

Related hazard in this container: the real process environment carries `AGTK_GH_USER`,
`AGTK_GH_EMAIL` and `AGTK_SIGNING_KEY_B64`. Every test of the command builds its environment
explicitly (temp `HOME`, cleared `GIT_CONFIG_GLOBAL`, the three variables set or emptied), because
one that inherits them would rewrite the real `~/.gitconfig` and `~/.ssh`.
