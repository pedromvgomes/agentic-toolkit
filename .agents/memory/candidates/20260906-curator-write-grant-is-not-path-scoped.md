---
about: the curator's argv grant scopes deletion and stamping but not Write/Edit, so "bounded by code, not prose" holds only for part of it
saw:
  - internal/curator/curator.go
  - internal/curator/prompt.md
  - docs/adr/0004-the-curator-ships-in-the-binary.md
---

Kind: **gotcha**.

`docs/adr/0004-the-curator-ships-in-the-binary.md` states that for the curator the
single-writer rule "becomes enforcement — the tool grant is constructed in Go at the call
site as `--allowedTools`, an argv flag no settings file can widen, so the curator's own
authority over `notes/` is bounded by code rather than by prose." `internal/curator/curator.go:186-198`
repeats the claim in its own words.

The grant does not carry that much. `allowedTools` (`internal/curator/curator.go:199-241`)
appends bare `"Write"` and `"Edit"` — no path argument, no store prefix — alongside
`permissionMode = "acceptEdits"` (`internal/curator/curator.go:274`). Only two members of
the list are actually scoped:

- deletion: `"Bash(rm "+candidatesDir+"/*)"` (`:238`), skipped entirely when the caller
  names no directory (`:237`);
- stamping: `anchorGrants` (`internal/curator/curator.go:257-266`), which lists note names
  individually for a `--notes` run.

So what is bounded by code is *deletion* and *stamping*. Where the curator may write is
bounded by prose: `internal/curator/prompt.md:63` ("One fact per file, at
`<root>/notes/<kebab-case-name>.md`") and `:133` ("Never hand-edit `INDEX.md`"). With
`Write` + `Edit` + `acceptEdits`, a curation run can write any file in the working
directory, including the consumer's source, `.claude/`, or the entry manifest.

What breaks when someone gets this wrong: anyone who trims a scoping sentence out of
`prompt.md` on the grounds that "the grant enforces it" removes the only thing that
confines writes. Same failure for anyone who reasons from ADR 0004 that `agtk memory
curate` is safe to run over candidate bodies whose content they have not read — candidate
markdown is model-authored input that the curator reads and acts on, and nothing in the
grant stops an instruction in one from being carried out with `Write`.

The honest split, and the one worth writing down before the next change to either file:
deletion and stamping are enforced; the write surface is instructed.
