---
name: curator-write-grant-is-not-path-scoped
kind: gotcha
description: The curator's argv grant scopes deletion and stamping but grants bare Write/Edit, so where it may write is prose, not code.
anchors:
  - path: internal/curator/curator.go
    blob: 0ba761a0826f
  - path: internal/curator/prompt.md
    blob: e99c915ac4dd
  - path: docs/adr/0004-the-curator-ships-in-the-binary.md
    blob: 39ec9633f9e1
confidence: verified
---

`docs/adr/0004-the-curator-ships-in-the-binary.md:42-43` says the tool grant "is constructed
in Go at the call site as `--allowedTools`, an argv flag no settings file can widen, so the
curator's own authority over `notes/` is bounded by code rather than by prose."
`internal/curator/curator.go:186-198` repeats it.

The grant does not carry that much. `allowedTools` (`internal/curator/curator.go:199`)
appends bare `"Write"` and `"Edit"` at `:221` — no path argument, no store prefix — alongside
`permissionMode = "acceptEdits"` (`:279`). Only two members of the list are actually scoped:

- deletion: `"Bash(rm "+candidatesDir+"/*)"` (`:238`), skipped entirely when the caller names
  no directory (`:237`);
- stamping: `anchorGrants` (`:262`), which lists note names individually for a `--notes` run —
  see [[scoped-anchor-grant-names-each-note-exactly]].

So *deletion* and *stamping* are bounded by code. Where the curator may write is bounded by
prose, in `internal/curator/prompt.md:63` ("One fact per file, at
`<root>/notes/<kebab-case-name>.md`") and `:137` ("Never hand-edit `INDEX.md`"). With
`Write` + `Edit` + `acceptEdits`, a curation run can write any file in the working directory,
including the consumer's source, `.claude/`, or the entry manifest.

Two ways to get burned. Trimming a scoping sentence out of `prompt.md` because "the grant
enforces it" removes the only thing confining writes. And reasoning from ADR 0004 that
`agtk memory curate` is safe over candidate bodies you have not read: candidate markdown is
model-authored input the curator reads and acts on, and nothing in the grant stops an
instruction in one from being carried out with `Write`.
