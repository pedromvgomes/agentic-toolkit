---
name: curator-write-grant-is-spelled-edit-with-no-mode
kind: gotcha
description: The curator's write grant is path-scoped, and both `Write(...)` instead of `Edit(...)` and any permission mode silently unscope it.
anchors:
  - path: internal/curator/curator.go
    blob: c1d5e5e32814
  - path: internal/curator/prompt.md
    blob: e99c915ac4dd
  - path: docs/adr/0004-the-curator-ships-in-the-binary.md
    blob: 39ec9633f9e1
confidence: verified
---

`allowedTools` (`internal/curator/curator.go:204`) builds a path-scoped grant, and the two
ways to un-scope it by accident are both non-obvious:

1. **Spelling.** The write grant is `"Edit("+editPattern(notesDir)+")"`
   (`curator.go:237`, `editPattern` at `:306` rendering `dir + "/**"`, doubling the leading
   slash for an absolute path). The comment at `curator.go:226-229` explains why it is never
   spelled `Write(...)`: an `Edit` rule covers every file-editing tool *including* `Write`,
   while a `Write` rule is not consulted by the file permission check at all. A grant written
   the obvious way names the right path and constrains nothing.
2. **Permission mode.** `permissionMode = ""` (`curator.go:325`), deliberately. The comment at
   `:313-324` is the reason: a waiving mode outranks `AllowedTools` rather than combining with
   it, and `acceptEdits` in particular approves every file edit anywhere on disk — the notes
   directory named in the `Edit` rule stops being a boundary and becomes a comment. Setting a
   mode here hollows out the enforcement claim ADR 0004 makes at
   `docs/adr/0004-the-curator-ships-in-the-binary.md:41-43` ("the tool grant is constructed in
   Go at the call site as `--allowedTools`, an argv flag no settings file can widen").

Three grants are withheld entirely rather than composed from an empty directory, because an
empty `notesDir`/`candidatesDir` would compose to a pattern rooted at `/`: the `Edit` grant
(`curator.go:236-237`), candidate deletion `Bash(rm <candidatesDir>/*)` (`:255-256`), and note
retraction `Bash(rm <notesDir>/*)` (`:263-264`). A caller naming no directory gets a run that
promotes or deletes nothing — visible — rather than a licence over the repo.

What is *not* bounded by the grant: which file inside `notesDir` a run writes, and whether it
hand-edits `INDEX.md`, remain prose in `internal/curator/prompt.md`. And candidate markdown is
model-authored input the curator reads and acts on; nothing in the grant stops an instruction
in one from being carried out with the `Edit` it holds over the notes directory.

Related: [[scoped-anchor-grant-names-each-note-exactly]], on the stamping half of the same
grant (`anchorGrants`, `curator.go:288`).
