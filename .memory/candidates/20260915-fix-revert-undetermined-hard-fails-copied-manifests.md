---
about: a non-Builtin manifest hard-errors on an undetermined escalate condition instead of skipping — and this repo's own manifest.yaml is such a non-Builtin manifest that names fix-revert/bugfix-lines, copied verbatim from default.yaml
saw:
  - source/toolkit/internal/review/symbols.go
  - source/toolkit/internal/review/selection.go
  - source/toolkit/internal/review/manifest.go
  - source/toolkit/internal/review/default.yaml
  - .agentic-toolkit/code-review/manifest.yaml
  - definitions/CONFIG-SCHEMA.md
---

`agtk code-review explain --json` used to fail on large multi-commit diffs (25+ hunks) with
`escalate[0]: signal \`fix-revert\` could not be determined`. Root cause and fix landed in the
same investigation (see `symbols.go`'s `detectFixRevert`): a `revertTailHunks = 25` shortcut
capped the revert search to 25 hunks past the first routine-repair ancestor found, instead of
spending the full `blameBudget` (200). Since a `fix:` ancestor turns up within the first few
hunks of almost any real branch, this made `fix-revert` (and, on a shorter path, `bugfix-lines`)
read as undetermined on ordinary diffs — the shortcut has been removed; the search now always
spends the full budget looking for a revert, independent of whether a routine repair already
answered `bugfix-lines`.

What is still true and still worth knowing:

- `selection.go:169`: `if undecided && !m.Builtin { return nil, fmt.Errorf(...) }` — the
  embedded default manifest (`Builtin: true`) merely skips a rule it can't evaluate; any
  manifest loaded from a repo's own `.agentic-toolkit/code-review/manifest.yaml` (`Builtin:
  false`) hard-errors `code-review explain` instead. This split is deliberate (see the comment
  on `Manifest.Builtin`) but nothing documents it as something a repo authoring its own manifest
  should design around.
- This repo's own `.agentic-toolkit/code-review/manifest.yaml` is a near-verbatim copy of
  `source/toolkit/internal/review/default.yaml` (also reproduced as the worked example in
  `definitions/CONFIG-SCHEMA.md:287-352`) — it keeps all eight `fix-revert`/`bugfix-lines`
  escalate rules unchanged, and is a non-Builtin manifest. The fix above removes the common
  trigger, but a diff that genuinely exhausts the full 200-hunk budget would still hard-error
  here rather than skip, precisely because this manifest is not the built-in one. Neither
  `default.yaml`'s comments nor `CONFIG-SCHEMA.md` warn a reader off keeping these rules
  verbatim in a repo-authored manifest.
- No ADR or memory note covered any of this before now (checked `docs/adr/*.md` — 0011, 0012,
  0015 are the only escalate-adjacent ones — and all prior memory notes/candidates).
