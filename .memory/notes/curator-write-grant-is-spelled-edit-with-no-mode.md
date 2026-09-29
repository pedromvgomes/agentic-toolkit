---
name: curator-write-grant-is-spelled-edit-with-no-mode
kind: gotcha
description: The curator's write grant is path-scoped, and both `Write(...)` instead of `Edit(...)` and any permission mode silently unscope it.
anchors:
  - path: source/toolkit/internal/curator/curator.go
    blob: cb07c4d5ea3f
  - path: source/toolkit/internal/curator/prompt.md
    blob: fe8576dcadf8
  - path: docs/adr/0004-the-curator-ships-in-the-binary.md
    blob: 123f8eae6042
confidence: verified
---

`allowedTools` (`source/toolkit/internal/curator/curator.go:311`, doc `:297-310`) builds a
path-scoped grant, and the two ways to un-scope it by accident are both non-obvious:

1. **Spelling.** The write grant is `"Edit("+editPattern(notesDir)+")"` (`curator.go:344`), with
   `editPattern` at `:413` rendering `dir + "/**"` (doubling the leading slash for an absolute
   path). The comment at `:333-337` explains why it is never `Write(...)`: an `Edit` rule covers
   every file-editing tool including `Write`, while a `Write` rule is not consulted by the file
   permission check at all.
2. **Permission mode.** `const permissionMode = ""` (`curator.go:511`, reasoning `:499-510`): a
   waiving mode outranks `AllowedTools` rather than combining with it, and `acceptEdits` approves
   every file edit anywhere on disk. Setting a mode hollows out ADR 0004's claim
   (`docs/adr/0004-the-curator-ships-in-the-binary.md:42-43`: the grant is an argv flag no
   settings file can widen).

Three grants are withheld rather than composed from an empty directory, which would compose to a
pattern rooted at `/`: the `Edit` grant (`curator.go:343-345`, rationale `:338-342`), candidate
deletion `Bash(rm <candidatesDir>/*)` (`:362-364`), note retraction `Bash(rm <notesDir>/*)`
(`:370-372`). A caller naming no directory gets a run that promotes nothing, visibly.

A grant is necessary but not sufficient: see [[bash-writes-are-refused-inside-the-working-directory]].

Not bounded by the grant: which file inside `notesDir` is written and hand-editing `INDEX.md`
remain prose (`prompt.md:66`, `:144`). The only `Edit` rule is on the notes directory, so the
curator can delete a candidate but not rewrite one. Candidate markdown is model-authored input;
nothing in the grant stops an instruction in one being carried out with that `Edit`.

Related: [[scoped-anchor-grant-names-each-note-exactly]] (`anchorGrants`, `curator.go:395`).
