---
description: "A function that can fail after producing output a caller needs must return that output alongside the error, and the caller must check for it before falling back to a bare error message."
---

# Don't let a returned error discard output produced before the failure

A command can fail in two ways: "nothing ran", and "it ran and produced something worth showing,
and *then* a later check on that output failed." Collapsing
both into the same `if err != nil { return err }` silently drops whatever the first mode had
that the second mode doesn't: the operator loses the one account of what a run actually did, at
exactly the moment they need it most to understand why the check that came after it failed.

## Applies to

Any `RunE`/command handler in `source/toolkit/internal/cli` that wraps a call able to return a
populated result *and* an error together — e.g. `curator.Run`, whose `Result` is populated
before a verification failure is returned beside it. When a function can fail in more than one
place, check which of its error returns still carry a populated result and print those before
returning, rather than assuming every error path is the empty one.

## Example

Good: `reportCurateResult` in `memory.go` checks `res.Text` — non-empty means the error is a
verification failure with the curator's own account attached, so it prints `res.Text` (or the
JSON report carrying it) before returning the error. Every other error path returns before
`Text` is ever set, so its emptiness is what tells the two apart.

Bad: `if err != nil { return err }` immediately after the call, before ever looking at whether
the accompanying result has something in it worth surfacing.
