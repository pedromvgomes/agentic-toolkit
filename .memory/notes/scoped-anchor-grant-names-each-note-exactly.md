---
name: scoped-anchor-grant-names-each-note-exactly
kind: invariant
description: A scoped stamping grant lists each note name with no trailing wildcard, because kebab-case note names nest.
anchors:
  - path: source/toolkit/internal/curator/curator.go
    blob: a8ac0f37cab8
  - path: source/toolkit/internal/memory/lint.go
    blob: 55ed2c6534eb
confidence: verified
---

`anchorGrants` (`source/toolkit/internal/curator/curator.go:300`) emits one grant per note as
`Bash(<agtk> memory anchor <name>)` with **no trailing wildcard**, so a scoped run stamps one
note per call. An unscoped run gets the open `anchor*` form, since its scope is the store.

The wildcard is the trap. Note names are kebab-case — `nameRe` at `source/toolkit/internal/memory/lint.go:14`
is `^[a-z0-9]+(-[a-z0-9]+)*$` — so one name can be a proper prefix of another. A grant reading
`Bash(<agtk> memory anchor lockfile-pins*)` also permits
`anchor lockfile-pins-shas-not-tags`, a different note the run never checked. The reasoning
that a trailing `*` "stops short of a second note name because a name cannot contain a space"
is wrong: it stops short of a second *argument*, not of a longer name in the same argument.

That leak is not small. `agtk memory anchor` clears the staleness signal, the one thing that
would have told the next reader nobody has verified a claim. A note silently marked fresh is
worse than a stale one, because no later audit flags it again — the failure ADR 0003 exists
to prevent, reintroduced through the grant meant to enforce it.

`TestAScopedRunCanOnlyStampTheNotesItNames`
(`source/toolkit/internal/curator/tests/run_test.go:299-308`) asserts the scoped run holds the exact grant
and not the open `anchor*` one, and `TestAScopedStampingGrantDoesNotReachPrefixedNames`
(`:314-322`) asserts no stamping grant in a scoped run ends in `*`. The reasoning is also
written down beside the code, at `source/toolkit/internal/curator/curator.go:288-299`.

Related: [[curator-write-grant-is-spelled-edit-with-no-mode]], on what the rest of that grant
does and does not bound.
