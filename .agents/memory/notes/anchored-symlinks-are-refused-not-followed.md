---
name: anchored-symlinks-are-refused-not-followed
kind: invariant
description: The lexical anchor check is backed by a filesystem containment check at every resolution site, so a symlink in the anchored set is refused rather than hashed through.
anchors:
  - path: internal/memory/lint.go
    blob: 55ed2c6534eb
  - path: internal/memory/anchor.go
    blob: a8d7491d9e82
  - path: internal/memory/audit.go
    blob: b7fa6df6e433
  - path: internal/memory/blob.go
    blob: 5a7a3778d65a
confidence: verified
---

`ValidateAnchorPath` (`internal/memory/lint.go:26`) is purely lexical — `**`, `filepath.IsAbs`,
and `filepath.Clean` tested against `..`. It never touches the filesystem, so on its own it
cannot see a symlink, and the confinement it promises at `:23-25` would be a property of path
*strings* only.

**It is not on its own.** `contained` (`internal/memory/anchor.go:148`) resolves the whole path
with `filepath.EvalSymlinks`, resolves `ProjectRoot` the same way, and rejects anything whose
relative path is `..` or starts with `../` (`:149-162`). Every resolution site gates on it:

- concrete anchors: `hashRegularFile` (`anchor.go:119`) uses `os.Lstat` (`:120`), not `os.Stat`,
  requires `IsRegular` (`:127`), and gates on `contained` (`:130-132`) before reaching
  `HashFile` (`:133`);
- glob anchors: `globFiles` (`anchor.go:173`) applies `Lstat` + `IsRegular` + `contained` per
  match (`:188-191`) and skips anything that fails;
- audit: a leaf symlink is refused outright as `DriftInvalid` "is a symlink, which is not
  anchorable" (`internal/memory/audit.go:96-104`), and containment is checked at `:105-111`
  *before* `HashFile` at `:112`, because `HashFile` is an `os.ReadFile`
  (`internal/memory/blob.go:31-32`) that resolves every component.

Two reasons the obvious shortcuts are wrong, both written down beside the code:

- **`Lstat` on the final component is not enough** (`anchor.go:143-147`). The kernel resolves
  every directory above it, so `internal/x -> ~/.ssh` with the anchor `internal/x/id_rsa` names
  a genuine regular file. The whole path has to be resolved, not its last segment — which is
  why a glob is the easier way in: `filepath.Glob` walks through a linked directory component
  and the pattern never names the link (`anchor.go:183-187`).
- **Audit must refuse what stamping refuses** (`audit.go:92-95`). Stamping a refused anchor
  leaves its blob empty, so an audit that skipped the check would read the outside file on
  every run rather than never.

Do not weaken either half on the assumption the other covers it: the lexical check is what
keeps a written-down anchor honest, and `contained` is what keeps the filesystem from
contradicting it. Pinned by `TestAuditRefusesAnAnchorUnderASymlinkedDirectory` and its
neighbours in `internal/memory/tests/anchor_test.go:331`.

Supersedes an earlier note that recorded this as an open gap ("anchor confinement is lexical
only"); the gap was closed and the note inverted.
