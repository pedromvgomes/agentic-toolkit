---
about: anchor confinement to the project root is a lexical path check; a symlink inside the tree is followed and hashes content from outside it
saw:
  - internal/memory/lint.go
  - internal/memory/anchor.go
  - internal/memory/blob.go
---

Kind: **gotcha**.

`ValidateAnchorPath` (`internal/memory/lint.go:26-37`) states the guarantee at `:23-25`:
"Anchors are also confined to the project: an absolute path, or one that climbs out with
`..`, would record hashes — and, for a glob, the file names themselves — from outside the
repo into a committed note."

The check is purely lexical: `strings.Contains(path, "**")`, `filepath.IsAbs`, and
`filepath.ToSlash(filepath.Clean(path))` against `".."` / `"../"`. It never touches the
filesystem, so it cannot see a symlink.

Every resolution path that follows uses stat-that-follows rather than `Lstat`:

- concrete anchors: `hashRegularFile` (`internal/memory/anchor.go:118-130`) calls
  `os.Stat(abs)`, accepts `info.Mode().IsRegular()`, then `HashFile`
  (`internal/memory/blob.go:31-37`) `os.ReadFile`s it.
- glob anchors: `globFiles` (`internal/memory/anchor.go:140-158`) calls `os.Stat(hit)` on
  each match with the same `IsRegular()` test.
- audit takes the same route via `HashFile` (`internal/memory/audit.go:75`) and
  `expandGlob` (`:95`).

`os.Stat` resolves the link, so a symlink at a project-relative path whose target is outside
the repo reports as a regular file and is read and hashed. Checked the semantics directly:
a `proj/link.txt -> ../outside.txt` reports `isfile == True` with a regular-file mode.

What breaks when someone gets this wrong: the confinement claim is what a reviewer relies on
when reading a committed note's `anchors:` block — the paths are supposed to be enough to
tell what the note watches. A glob anchor over a directory containing a symlink records the
*target's* content hash under the in-repo path, so a note can go stale (or stay green)
because of a file nobody reviewing the repo can see. It also means "the store never records
anything from outside the repo" is not a property the code establishes, only one the
lexical check establishes for path strings.

Not exploited by anything today — this is about what the comment promises versus what the
check delivers, and about not weakening the lexical check on the belief that something
downstream re-verifies containment. Nothing does.
