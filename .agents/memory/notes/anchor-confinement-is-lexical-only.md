---
name: anchor-confinement-is-lexical-only
kind: gotcha
description: Anchor confinement is a string check only, so a symlink inside the tree hashes content from outside the repo.
anchors:
  - path: internal/memory/lint.go
    blob: 4126a2258ab4
  - path: internal/memory/anchor.go
    blob: 0385f7c8e321
  - path: internal/memory/blob.go
    blob: 5a7a3778d65a
  - path: internal/memory/audit.go
    blob: 1d67b18de909
confidence: verified
---

`ValidateAnchorPath` (`internal/memory/lint.go:26`) promises at `:23-25` that "anchors are
also confined to the project: an absolute path, or one that climbs out with `..`, would
record hashes — and, for a glob, the file names themselves — from outside the repo into a
committed note."

The check is purely lexical — `strings.Contains(path, "**")`, `filepath.IsAbs`, and
`filepath.ToSlash(filepath.Clean(path))` tested against `".."` / `"../"`. It never touches
the filesystem, so it cannot see a symlink.

Every resolution path that follows stats through the link rather than using `Lstat`:

- concrete anchors: `hashRegularFile` (`internal/memory/anchor.go:118`) calls `os.Stat(abs)`
  at `:119`, accepts `info.Mode().IsRegular()`, then hands off to `HashFile`
  (`internal/memory/blob.go:31`), which `os.ReadFile`s it at `:32`;
- glob anchors: `globFiles` (`internal/memory/anchor.go:140`) applies the same
  `os.Stat`/`IsRegular` test per match at `:150`;
- audit takes the same route, via `HashFile` (`internal/memory/audit.go:75`) and
  `expandGlob` (`:95`).

`os.Stat` resolves the link, so a symlink at a project-relative path pointing outside the
repo reports as a regular file and is read and hashed under its in-repo path.

Why it matters: a reader of a committed note treats the `anchors:` block as the full
statement of what the note watches. With a symlink in the anchored set, a note can go stale —
or stay green — because of a file nobody reviewing the repo can see. So "the store never
records anything from outside the repo" is not a property the code establishes, only one the
lexical check establishes for path *strings*. Nothing downstream re-verifies containment;
do not weaken the lexical check on the assumption that something does.
