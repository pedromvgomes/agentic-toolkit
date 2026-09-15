---
about: revertTailHunks (25) commonly drives fix-revert/bugfix-lines to "undetermined" on diffs past ~25-30 tracked hunks, and a non-Builtin manifest hard-errors on that instead of skipping — and this repo's own manifest.yaml is such a non-Builtin manifest that names fix-revert/bugfix-lines
saw:
  - source/toolkit/internal/review/symbols.go
  - source/toolkit/internal/review/selection.go
  - source/toolkit/internal/review/manifest.go
  - source/toolkit/internal/review/default.yaml
  - .agentic-toolkit/code-review/manifest.yaml
  - definitions/CONFIG-SCHEMA.md
---

No ADR or memory note covers `revertTailHunks`, the `Builtin` hard-error/skip split in
`selection.go:169`, or manifest-authoring guidance to narrow/avoid copying `default.yaml`'s
escalate rules. Checked `docs/adr/*.md` (0011, 0012, 0015 are the only escalate-adjacent ADRs,
none discuss this) and all 39 memory notes/11 candidates — none anchor `symbols.go`,
`selection.go`'s Builtin branch, or this failure mode.

Mechanism, confirmed by reading:
- `symbols.go:167` `revertTailHunks = 25`: once a routine-repair ancestor is found for any
  hunk (`bugfixFound`), the remaining search for a revert is capped to 25 more hunks instead
  of the full `blameBudget` (200, `change.go:145`). Past that shortened limit both
  `fix-revert` and `bugfix-lines` are marked undetermined (`symbols.go:218-220` area).
- `selection.go:169`: `if undecided && !m.Builtin { return nil, fmt.Errorf(...) }` — the
  embedded default manifest (`Builtin: true`) merely skips a rule it can't evaluate; any
  manifest loaded from a repo's own `.agentic-toolkit/code-review/manifest.yaml` (`Builtin:
  false`) hard-errors `code-review explain` instead.
- `source/toolkit/internal/review/default.yaml` (also reproduced verbatim as the worked
  example in `definitions/CONFIG-SCHEMA.md:287-352`) ships six escalate rules keyed on
  `signals: {in: [auth, crypto, concurrency, sensitive-data, fix-revert]}` and two more on
  `[migrations, ci-cd, iac, bugfix-lines]`.
- This repo's own `.agentic-toolkit/code-review/manifest.yaml` is a near-verbatim copy of
  `default.yaml` (diffed the two directly) — it keeps all eight fix-revert/bugfix-lines
  escalate rules unchanged, and is a non-Builtin manifest. So agentic-toolkit's own repo
  manifest carries exactly the shape the bug report describes, and would hard-fail
  `code-review explain` on any of its own diffs exceeding ~25-30 tracked hunks past the first
  routine-fix ancestor.

This answers "was default.yaml ever a copy-paste starting template": in practice, yes — the
one manifest this repo has written independently of the embedded default is a copy of it, not
a narrower rewrite, and neither `default.yaml`'s comments nor `CONFIG-SCHEMA.md` warn a reader
off keeping the `fix-revert`/`bugfix-lines` escalate rules verbatim. No note or ADR treats the
`revertTailHunks` shortcut, the `Builtin` skip/error split, or this pairing as an accepted
tradeoff — it reads as an unaddressed gap, not a known/rejected one.
