---
name: non-builtin-manifest-hard-fails-undecided-escalate
kind: gotcha
description: A repo-authored code-review manifest hard-errors when an escalate rule cannot be evaluated, where the embedded default merely skips it, and this repo's own manifest copies the default's fix-revert rules verbatim.
anchors:
  - path: source/toolkit/internal/review/selection.go
    blob: fabfd46969de
  - path: source/toolkit/internal/review/manifest.go
    blob: 7a1ca075f74e
  - path: source/toolkit/internal/review/symbols.go
    blob: b59d967e2722
  - path: source/toolkit/internal/review/default.yaml
    blob: 9999a4078ff2
  - path: .agentic-toolkit/code-review/manifest.yaml
    blob: 86ca8ce2b250
confidence: verified
---

`selection.go:169`: `if undecided && !m.Builtin` returns an error ("Remove the rule, or narrow it
with `touches`"). The embedded default (`Builtin: true`, `manifest.go:84`) skips a rule it cannot
evaluate; a manifest loaded from a repo's own `.agentic-toolkit/code-review/manifest.yaml` fails
`code-review explain` instead. Deliberate, but nothing tells a repo copying the default to design
around it.

`.agentic-toolkit/code-review/manifest.yaml` and `default.yaml` each mention
`fix-revert`/`bugfix-lines` nine times, so this repo keeps the rules and is non-Builtin. History:
`explain --json` once failed on 25+ hunk diffs with "signal `fix-revert` could not be
determined" because a hunk-capped shortcut left the signal undetermined; that shortcut is gone
(`revertTailHunks` no longer appears in `symbols.go`). A diff that genuinely exhausts the full
blame budget would still hard-error here rather than skip.
