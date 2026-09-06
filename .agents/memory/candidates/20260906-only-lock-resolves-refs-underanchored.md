---
about: only-lock-resolves-refs quantifies over internal/cli but anchors five named files, so a new command cannot make it stale
saw:
  - internal/cli/*.go
targets: only-lock-resolves-refs
verdict: still-true
---

The claim holds. `internal/cli/lock.go` and the relock branch of `internal/cli/sync.go`
construct `LiveProvider`; `fetch.go`, `plan.go`, `render.go` and `status.go` construct the
frozen one. Nothing in `internal/cli/` resolves refs outside those two.

What does not hold is the anchoring. The note's own body names the failure mode as "picking
the wrong provider in a **new command**" — a file that does not exist yet — and the anchors
are the five files that exist today. A sixth `internal/cli/*.go` constructing a `LiveProvider`
falsifies the invariant and `agtk memory audit` reports nothing, so the note stays green while
the thing it protects is gone.

The claim quantifies over a file set: *every command except `lock` uses the frozen provider*.
Per `docs/adr/0005-glob-anchors-mark-quantified-claims.md` that is the trigger for a glob
anchor, so `internal/cli/*.go` is the anchor this note wants, not the five paths.

Two body pointers also need checking against the current tree, and `internal/cli/sync.go` is
cited in the body without appearing in `anchors:` at all.
