---
about: a catalog command at definitions/commands/foo/bar.md is named "bar" (filename stem only) unless its frontmatter declares name:, while a local.commands scan derives "foo/bar" from the relative path — the two namespacing rules disagree for nested files
saw:
  - source/toolkit/internal/definitions/parser.go
  - source/toolkit/internal/resolver/local.go
  - source/toolkit/internal/resolver/resolver.go
---

`ParseFile` (`definitions/parser.go`) derives a file-shaped definition's fallback name via
`stripExt(filename)`, called with just the bare filename component the caller passes in. For a
catalog entry, `bareLayout`'s existing per-category directory walk
(`source/toolkit/internal/resolver/resolver.go`) lists `definitions/commands/foo/bar.md` and
passes only `"bar.md"` as `fileName` — the `foo/` segment is never part of what `stripExt` sees,
so the derived name is `"bar"`, not `"foo/bar"`. The nested path only becomes the command's name
if the file's own frontmatter sets `name: foo/bar` explicitly.

`local.commands`' scan (`resolver/local.go`, `localCommandFiles`) does the opposite by design:
it walks the scan directory recursively and passes the **relative path including subdirectories**
(e.g. `"foo/bar.md"`) as `fileName` to `parseFromFS` → `ParseFile` → `stripExt`, so the derived
name is `"foo/bar"` — namespaced by directory structure with no frontmatter required. This
matches the local-scan spec's stated behavior ("nested files are namespaced") but means a
catalog command and a locally-scanned command at the same nested path compute different names
unless the catalog file's frontmatter is written to match. A test asserting the two collide
(`local.commands` overriding a catalog command) had to give the catalog file an explicit
`name: foo/bar` frontmatter field to make them collide at all — without it, they'd coexist as
`"bar"` and `"foo/bar"`, two different commands, not one overriding the other.

This asymmetry was flagged during implementation, not fixed — the local-scan-only fallback
mirrors the local: feature's explicit design (namespacing nested commands by directory is a new
capability being added for local: specifically), while retrofitting the same behavior onto
catalog-listed commands was out of scope and would be a separate, likely breaking, change to
existing bareLayout-derived names.
