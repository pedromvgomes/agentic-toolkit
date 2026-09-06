---
about: a scoped stamping grant must name each note exactly, because kebab-case note names nest
saw:
  - internal/curator/curator.go
  - internal/memory/lint.go
---

**invariant.** `anchorGrants` in `internal/curator/curator.go` emits one grant per note as
`Bash(<agtk> memory anchor <name>)` with **no trailing wildcard**, and a scoped run therefore
stamps one note per call.

The wildcard form is the trap. Note names are kebab-case — `nameRe` at
`internal/memory/lint.go:14` is `^[a-z0-9]+(-[a-z0-9]+)*$` — so one name can be a proper
prefix of another. A grant reading `Bash(<agtk> memory anchor lockfile-pins*)` also permits
`agtk memory anchor lockfile-pins-shas-not-tags`, a different note the run never checked.

That is not a small leak. `agtk memory anchor` clears the staleness signal, which is the one
thing that would have told the next reader nobody has verified the claim. A note silently
marked fresh is worse than a stale one, because no later audit flags it again — the failure
ADR 0003 exists to prevent, reintroduced through the grant that was meant to enforce it.

The reasoning that a trailing `*` "stops short of a second note name because a name cannot
contain a space" is wrong: it stops short of a second *argument*, not of a longer name in the
same argument.

Checked against `internal/curator/tests/run_test.go`, which asserts no scoped stamping grant
ends in `*)`.
