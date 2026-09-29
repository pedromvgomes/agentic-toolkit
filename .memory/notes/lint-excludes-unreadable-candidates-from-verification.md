---
name: lint-excludes-unreadable-candidates-from-verification
kind: rationale
description: Store.Lint leaves unreadable candidates out so the curator's verify() cannot fail on a file it cannot repair; the CLI adds them through Store.LintCandidates, and the steering is prose only.
anchors:
  - path: source/toolkit/internal/memory/lint.go
    blob: 69162c090427
  - path: source/toolkit/internal/curator/verify.go
    blob: deee1e17d6ff
  - path: source/toolkit/internal/curator/curator.go
    blob: cb07c4d5ea3f
  - path: source/toolkit/internal/curator/prompt.md
    blob: fe8576dcadf8
  - path: docs/adr/0022-agtk-clears-the-candidates-the-curator-processed.md
    blob: 10f104a761bd
confidence: verified
---

`verify()` calls `store.Lint(notes, nil)` (`curator/verify.go:153`) and fails a run for a lint issue
on a note it reported touched (`touchedProblems`, `verify.go:176`); candidate issues added to
`Lint` would land in that list. The curator cannot repair a candidate: its only `Edit` grant is the
notes directory (`curator.go:344`), and for candidates it holds only `Bash(rm <candidatesDir>/*)`
(`curator.go:362-363`). Deleting is the sole action an issue could provoke, and that loses the
finding or fails verification as an unreported removal (`verify.go:201-205`).

So the check is a separate method, `Store.LintCandidates` (`memory/lint.go:117`, doc `:108-116`),
and the CLI appends it after `Store.Lint` (`cli/memory.go:472`). `TestLintSaysNothingAboutAnUnreadableCandidate`
(`memory/tests/candidate_test.go`) pins that `Lint`'s output does not change. ADR 0022 records this.

The curator's grant includes `Bash(agtk memory lint*)` (`curator.go:327`) and `prompt.md:127` says
to run it and "fix anything it reports". That CLI command does append `LintCandidates`, so its
output lists unreadable candidates; `prompt.md:130-131` says those lines are not the curator's to
fix and stay out of its report, and `prompt.md:13-16` says the same of `candidates --json`'s
`unreadable` list (`TestThePromptTellsTheCuratorItsLintRunsUnreadableCandidatesAreNotItsToFix`,
`curator/tests/unreadable_test.go`). Prose only: `verify` cannot tell a deleted unreadable candidate
from a readable one, so a curator that deleted one and reported it resolved would pass.

`clearResolved` deletes only ids in the pre-run snapshot (`verify.go:258-268`), so a report id such
as `../notes/x` matches nothing and is never joined into a path (no test names one). `Run` filters
the snapshot through `withoutUnreadable` first (`curator.go:624`, `:635-652`), so an unreadable
candidate reported as resolved is not removed. See
[[unreadable-candidates-surface-in-four-places-and-parsing-stays-strict]].
