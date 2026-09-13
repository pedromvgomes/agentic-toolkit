---
about: internal/review stays model-free and reviewrun is the only model-invoking layer; reviewpost is the layer that may know GitHub exists
saw:
  - source/toolkit/internal/review/capability.go
  - source/toolkit/internal/reviewrun/run.go
  - source/toolkit/internal/reviewpost/*.go
  - source/toolkit/internal/cli/tests/deterministic_surface_test.go
targets: review-is-model-free-reviewrun-is-not
verdict: still-true
---

Re-checked because the note is marked stale (four anchored files' blobs moved:
builtin.go, errors.go, git.go, manifest.go in internal/review).

`grep -rl agentic-driver source/toolkit/internal/review` still returns exactly
`capability.go`; the comment there is still at `capability.go:3-9` and the import at `:14`,
unchanged from the note's pointers. `TestTheModelInvokingPackagesCannotReachTheCredential`
still passes. `reviewrun/run.go`'s package doc still says "Nothing here talks to GitHub."

Not covered by the existing note, and directly relevant to any feature that needs to call
GitHub after a run (e.g. resolving a thread whose finding is fixed):
`internal/reviewpost` is the layer that is allowed to import `internal/githubapp` — both
`reviewpost/threads.go` and `reviewpost/payload.go` do — precisely because it sits between
`reviewrun` (model, no GitHub) and the CLI transport. `reviewpost/threads.go`'s doc says it
plainly: "the translation lives beside the transport ... because internal/reviewrun never
learns that GitHub exists." New GitHub-side behaviour driven by a finished review's findings
belongs in `reviewpost` (or above it, in the CLI), never inside `reviewrun`.
