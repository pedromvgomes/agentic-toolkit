---
name: real-git-fixtures-live-in-two-test-packages
kind: gotcha
description: Only internal/cli/tests and internal/sourcestore/tests drive real git subprocesses; a slow package elsewhere is slow for some other reason.
anchors:
  - path: internal/cli/tests/*.go
    matches:
      - path: internal/cli/tests/config_flag_test.go
        blob: dbba3a1c8374
      - path: internal/cli/tests/default_stack_render_test.go
        blob: 7f51db16a363
      - path: internal/cli/tests/deterministic_surface_test.go
        blob: 9a338bc6fd2f
      - path: internal/cli/tests/fetch_test.go
        blob: 166471dd7188
      - path: internal/cli/tests/frozen_test.go
        blob: 504ce580b265
      - path: internal/cli/tests/helpers_test.go
        blob: 07d966c84806
      - path: internal/cli/tests/init_test.go
        blob: a82934434e00
      - path: internal/cli/tests/lock_test.go
        blob: 8cbd08996702
      - path: internal/cli/tests/memory_test.go
        blob: 54345d5d6155
      - path: internal/cli/tests/plan_json_test.go
        blob: f3510808b6f7
      - path: internal/cli/tests/plan_test.go
        blob: 5d4af8c99e9d
      - path: internal/cli/tests/render_test.go
        blob: 3b48b0796841
      - path: internal/cli/tests/source_flag_test.go
        blob: 7d1d2de69846
      - path: internal/cli/tests/status_test.go
        blob: 2a5cde27be35
      - path: internal/cli/tests/sync_test.go
        blob: e5c2a912dc7a
      - path: internal/cli/tests/update_test.go
        blob: d11d0e3df715
  - path: internal/sourcestore/tests/*.go
    matches:
      - path: internal/sourcestore/tests/annotated_tag_test.go
        blob: 96e48bfc84dd
      - path: internal/sourcestore/tests/frozen_provider_test.go
        blob: d590f8c51f87
      - path: internal/sourcestore/tests/helpers_test.go
        blob: 3d68486cde48
      - path: internal/sourcestore/tests/live_provider_test.go
        blob: 65a2ccbc182d
  - path: internal/curator/tests/*.go
    matches:
      - path: internal/curator/tests/curator_test.go
        blob: 2f438fa54b1f
      - path: internal/curator/tests/run_test.go
        blob: 093ce91e5390
confidence: verified
---

Two test packages build fixture repositories on disk and shell out to real `git`, and no
others do — `grep -rl 'exec.Command("git"' --include='*_test.go' .` returns
`internal/cli/tests/helpers_test.go` and `internal/sourcestore/tests/helpers_test.go` and
nothing else.

- `internal/cli/tests/helpers_test.go`: `fixtureRepoFromDir` at `:33` runs `git init`,
  `add`, `commit`, `clone --bare` and `rev-parse` through `runGitOK` (`:109`, exec at `:111`)
  and `runGitOut` (`:121`, exec at `:123`). It is called from `plan_test.go`, `sync_test.go`,
  `lock_test.go`, `fetch_test.go`, `status_test.go` and more, so a run builds dozens of
  repositories.
- `internal/sourcestore/tests/helpers_test.go`: `fixtureRepo` at `:36`, with the exec sites at
  `:103` (`runGitOK`) and `:115` (`runGitOut`). Note the two helpers are *not* the same name:
  `fixtureRepoFromDir` is the CLI package's.

Neither can be stubbed without testing nothing — git behaviour is what those two packages
exist to cover.

The trap is inferring the converse. `internal/curator/tests` is also slow and runs no git at
all: its cost is `agentictest.Fake.Build` (`run_test.go:10`, `:43`, from
`github.com/pedromvgomes/agentic-driver` pinned at `go.mod:8`), which writes a `/bin/sh`
script to a `t.TempDir()` and execs it once per case. "Slow package" and "git package" are
different sets.

`-short` does not help: `grep -rn 'testing.Short' --include='*.go' .` has no hits anywhere in
the repo. `make check` is `fmt vet test` (`Makefile:24`), and `go test ./...` runs packages in
parallel, so the suite's wall clock is set by its single slowest package rather than by the
sum — speeding up any one of the others moves nothing. Iterate with `go test` on the package
you are editing and keep `make check` for the end.
