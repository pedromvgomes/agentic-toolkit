---
about: memory.New accepts an absolute or climbing root; confinement lives in the separate ValidateRoot that only the CLI calls
saw:
  - internal/memory/store.go
  - internal/cli/memory.go
---

Kind: **invariant** (a call-order one).

`memory.New(projectRoot, root)` (`internal/memory/store.go:31-40`) joins a relative root
onto `projectRoot` and takes an absolute root as-is. It performs no checking at all.

The confinement rule lives in `ValidateRoot` (`internal/memory/store.go:46-57`), a
free function that rejects an absolute root and one that climbs out with `..`, with the
reason stated at `:42-45`: the store is meant to be committed and to travel with its
branch, and either shape silently defeats both.

There is exactly one caller pairing them, in the CLI: `internal/cli/memory.go:134-137`
runs `ValidateRoot(root)` and only then `memory.New(...)`. Nothing in the `memory` package
couples the two — `New`'s doc comment does not mention `ValidateRoot`, and no test in
`internal/memory/tests/` asserts the pairing.

What breaks when someone gets this wrong: any second construction site — a new subcommand,
a hook helper, the curator's own path handling — that calls `memory.New` with a
manifest-supplied `memory.root` and skips `ValidateRoot` produces a Store rooted outside
the repo. Every subsequent operation follows: `Scaffold` creates directories there,
`WriteIndex` and `Stamp` write there, and `RecordHit`'s `ensureGitignore`
(`internal/memory/hits.go:30`) creates the root as a side effect of a plain read. The
symptom is a store that lint reports as green while the repo it is supposed to travel with
has nothing committed in it.

If this pairing is meant to hold, the durable fix is folding the check into `New` (returning
an error) rather than remembering it at each call site.
