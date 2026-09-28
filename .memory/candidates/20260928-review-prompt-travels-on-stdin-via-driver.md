---
about: the review prompt (with the embedded diff) travels via agentic-driver's Invocation.Stdin, not argv, on both providers — and only because this repo's driver version is >= v0.10.0
saw:
  - source/toolkit/go.mod
  - source/toolkit/internal/reviewrun/invoke.go
  - source/toolkit/internal/curator/tests/codex_test.go
  - source/toolkit/internal/curator/tests/run_test.go
---

Linux caps a single argv element at 128 KiB (`MAX_ARG_STRLEN`); a review prompt
embedding a whole diff routinely exceeds that and fails at exec with
`fork/exec ...: argument list too long`. macOS only enforces an aggregate
~1 MiB `ARG_MAX` across the whole vector, so the identical request can pass
there while failing on Linux.

`github.com/pedromvgomes/agentic-driver` is pinned at v0.10.0
(`source/toolkit/go.mod:15`) — not vendored, so read it from the module cache,
not `source/`. As of that version `agentic.Invocation` carries a `Stdin []byte`
field (`provider.go:465-469` in the module: "a prompt carrying a whole diff
exceeds \[128 KiB] and fails at exec"), and `driver.go:461-462` pipes it to the
child's stdin (`cmd.Stdin = bytes.NewReader(inv.Stdin)`) when non-nil. Both
dialects put `req.Prompt` there and nowhere else:

- claudecode: `StreamCommand` returns `Invocation{Args: ..., Stdin:
  []byte(req.Prompt)}` — the prompt is not one of the args (`claudecode/provider.go`
  around its `StreamCommand` doc comment, "The prompt goes on stdin, which
  `claude -p` reads when no prompt argument follows it").
- codex: same shape, `codex/provider.go`'s `StreamCommand`: "The prompt goes on
  stdin and never into argv, so nothing it contains can be read as a flag."

This is a dependency-version fact, not something this repo's own code
constructs: argv/stdin assembly lives entirely inside the driver's per-provider
dialect, so nothing in `source/toolkit/internal/reviewrun` or
`internal/provider` would change if the split moved again. Pinning
`agentic-driver` back below v0.10.0 puts the prompt back on argv and
reintroduces the Linux-only E2BIG failure with no local code change to explain
why — `go.mod`'s version line is the only guard.

Schema payloads are unaffected: claudecode's `SchemaArgs` still returns the raw
schema string as an argv element, and codex's still writes it to a temp file
and passes `--output-schema <path>` — neither uses `Stdin`. The review schemas
are small enough (`source/toolkit/internal/reviewrun/schema.go`) that this was
never the risk; the diff embedded in the prompt was.

**Testing gotcha**: because the prompt no longer appears in argv for either
dialect, a test that wants to assert on prompt content must read
`agentictest.Fake.Stdin(t)`, not scan `fake.Recorded(t).Args`. Scanning argv
for prompt text now finds nothing regardless of what was actually sent.
`source/toolkit/internal/curator/tests/codex_test.go` and
`.../run_test.go` both assert against `Stdin(t)` for this reason — the
non-prompt assertions in the same tests (sandbox mode, `-s read-only`, etc.)
correctly stay on `Recorded(t).Args`, since flags are still argv.
