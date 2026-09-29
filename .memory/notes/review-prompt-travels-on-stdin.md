---
name: review-prompt-travels-on-stdin
kind: gotcha
description: The review prompt reaches the provider on stdin, not argv, only because agentic-driver is at v0.10.0 or later; older versions fail on Linux with E2BIG for large diffs, and tests must read Fake.Stdin.
anchors:
  - path: source/toolkit/go.mod
    blob: 51d9a4381ec1
  - path: source/toolkit/internal/reviewrun/invoke.go
    blob: ae55e9629092
  - path: source/toolkit/internal/curator/tests/codex_test.go
    blob: ba801b19c1f7
  - path: source/toolkit/internal/curator/tests/run_test.go
    blob: 33c52b976f71
confidence: suspect
---

Linux caps a single argv element at 128 KiB, so a prompt embedding a whole diff fails at exec
with "argument list too long"; macOS only limits the aggregate, so the same request passes there.
`github.com/pedromvgomes/agentic-driver` is pinned at v0.10.0 (`source/toolkit/go.mod:15`), and
that version's dialects put `req.Prompt` in `Invocation.Stdin`. Argv/stdin assembly lives entirely
in the driver, so no local code changes if the split moves; `go.mod`'s version line is the only
guard against downgrading.

Testing: prompt text no longer appears in argv, so assert on `fake.Stdin(t)`, not
`fake.Recorded(t).Args` (as `curator/tests/codex_test.go:111` does); flag assertions stay on argv.

Suspect: the driver-internal claims (the `Stdin` field and the dialect code) were read from the
module cache in an earlier session and were not re-checked here; only the pin and the tests are.
