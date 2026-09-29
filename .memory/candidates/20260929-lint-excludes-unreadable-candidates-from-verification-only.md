---
about: "Store.Lint leaves unreadable candidates out so verify() cannot fail on them, and the curator's prompt tells it the lint command's unreadable-candidate lines are not its to fix; clearResolved deletes only ids in the pre-run snapshot"
saw:
  - source/toolkit/internal/memory/lint.go
  - source/toolkit/internal/curator/verify.go
  - source/toolkit/internal/curator/curator.go
  - source/toolkit/internal/curator/prompt.md
  - source/toolkit/internal/curator/tests/unreadable_test.go
  - source/toolkit/internal/cli/memory.go
  - docs/adr/0022-agtk-clears-the-candidates-the-curator-processed.md
---

Why `Store.Lint` does not include unreadable candidates:

- `verify()` calls `store.Lint(notes, nil)` (`curator/verify.go:153`) and fails a run for a lint
  issue on a note it reported touched (`touchedProblems`, `verify.go:176`). Candidate issues added
  to `Lint` would land in that same list.
- The curator cannot repair a candidate. Its only `Edit` grant is the notes directory
  (`curator.go:344`); for candidates it holds only `Bash(rm <candidatesDir>/*)`
  (`curator.go:362-363`). Deleting the file is the sole action an issue on a candidate could
  provoke, and that either loses the finding or fails verification as an unreported removal
  (`verify.go:201-205`).
- So the check lives in a separate method, `Store.LintCandidates` (`memory/lint.go:108-117`), and
  the CLI adds it after `Store.Lint` (`cli/memory.go:472`). Test
  `memory/tests/candidate_test.go` `TestLintSaysNothingAboutAnUnreadableCandidate` pins that
  `Lint`'s output does not change when one is present. ADR 0022 records the decision.

The curator's grant includes `Bash(agtk memory lint*)` (`curator.go:327`) and `prompt.md:127` tells
it to run `agtk memory lint` and "fix anything it reports". That is the CLI command, which does
append `LintCandidates`, so the curator's own lint run lists unreadable candidates. The prompt says
at that step (`prompt.md:130-131`) that those lines are not the curator's to fix and stay out of its
report, and `prompt.md:13-16` says the same of `candidates --json`'s `unreadable` list.
`TestThePromptTellsTheCuratorItsLintRunsUnreadableCandidatesAreNotItsToFix`
(`curator/tests/unreadable_test.go`) asserts the lint-step wording reaches the run through the
driver's stdin. The steering is prose only: `verify` does not tell a deleted unreadable candidate
from a deleted readable one, since the snapshot is a directory listing (`takeSnapshot`,
`verify.go:80`), so a curator that deleted one and reported it resolved would pass.

`clearResolved` deletes only ids present in the pre-run snapshot. It iterates
`before.candidates` and joins each id, a filename stem read from `candidates/`, into the delete
path (`verify.go:258-268`), so a report id such as `../notes/x` matches nothing and is never
joined into a path. No test names such an id. `Run` passes the snapshot through
`withoutUnreadable` first (`curator.go:624`, `:635-652`), so an unreadable candidate reported as
resolved is not removed.
