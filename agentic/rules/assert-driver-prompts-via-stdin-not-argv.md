---
description: Assert driver-invocation prompt content via Stdin(t), never by scanning Recorded(t).Args
---

# Assert driver invocation prompts through `Stdin(t)`, never by scanning `Recorded(t).Args`

`agentic-driver`'s claudecode and codex dialects put the prompt — which can embed a whole
diff — on `Invocation.Stdin`, never in argv, so a large prompt cannot trip the OS argv-length
limit (`MAX_ARG_STRLEN` on Linux, aggregate `ARG_MAX` on macOS). A test that greps
`Recorded(t).Args` for prompt text passes vacuously: the string is not there regardless of
what the run actually sent.

## Applies to

Go tests that use `github.com/pedromvgomes/agentic-driver/agentictest.Fake` to assert on
driver invocations — currently `source/toolkit/internal/curator/tests/` and
`source/toolkit/internal/reviewrun/`. Flags and other argv-only values (e.g. `-s read-only`)
still belong on `Recorded(t).Args`; only prompt/diff content lives on stdin.

## Example

```go
stdin := fake.Stdin(t)
if !strings.Contains(stdin, "# Memory Curator") {
    t.Errorf("the curator's policy did not reach the run: %q", stdin)
}
if !strings.Contains(argv, "-s\x00read-only") {
    t.Errorf("the dry run was not sandboxed: %q", argv)
}
```
