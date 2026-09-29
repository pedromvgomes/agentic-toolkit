---
about: "curator-write-grant note's claims hold but every curator.go pointer has moved"
saw:
  - source/toolkit/internal/curator/curator.go
  - source/toolkit/internal/curator/prompt.md
  - docs/adr/0004-the-curator-ships-in-the-binary.md
targets: curator-write-grant-is-spelled-edit-with-no-mode
verdict: still-true
---

Re-read `curator.go` at HEAD. Grant construction and permission-mode reasoning hold. Corrected
pointers (note's value in parentheses):

- `allowedTools` `:311` (`:216`), doc comment `:297-310`.
- Edit grant `"Edit("+editPattern(notesDir)+")"` at `:344` inside the `if notesDir != ""` guard
  `:343-345` (`:248-249`); the empty-directory rationale for withholding it is `:338-342`.
- Edit/Write spelling comment `:333-337` (`:238-241`).
- `editPattern` `:413`, doc comment `:406-412` (`:318`).
- `const permissionMode = ""` `:511` with its comment `:499-510` (`:398`, `:386-397`).
- Candidate `rm` grant `:362-364` (`:267-268`); note `rm` grant `:370-372` (`:275-276`).
- `anchorGrants` `:395` (`:300`). ADR 0004's argv-flag sentence ("the tool grant is constructed in
  Go at the call site as `--allowedTools`, an argv flag no settings file can widen") is at
  `docs/adr/0004-the-curator-ships-in-the-binary.md:42-43` (`:42`).

Same shifts for `scoped-anchor-grant-names-each-note-exactly`: `anchorGrants` `:395`, its
rationale comment `:382-394` (`:300`, `:288-299`); `nameRe` is at `memory/lint.go:15` (`:14`); its
tests are `curator/tests/run_test.go:396-409` (`TestAScopedRunCanOnlyStampTheNotesItNames`) and
`:411-421` (`TestAScopedStampingGrantDoesNotReachPrefixedNames`), not `:299-308` / `:314-322`.

The "not bounded by the grant" paragraph still holds. The prose it points at in `prompt.md` is
the note location `:66` ("One fact per file, at `<root>/notes/<kebab-case-name>.md`") and
`:144` ("Never hand-edit `INDEX.md`"). The grant's only `Edit` rule is on the notes directory
(`curator.go:343-345`); no `Edit` rule names `candidates/`, so the curator can delete a candidate
but not rewrite one.
