---
about: how review conventions and the referencing_files escalation key behave under a repo-owned manifest
saw:
  - source/toolkit/internal/review/manifest.go
  - source/toolkit/internal/reviewrun/prompt.go
  - source/toolkit/internal/review/language.go
  - source/toolkit/internal/review/condition.go
  - .agents/code-review/manifest.yaml
  - definitions/CONFIG-SCHEMA.md
---

Superseded version: this candidate originally claimed `.agents/code-review/manifest.yaml`
does not exist in this repo, backed by a `find` run before commit
79fc0b9 ("fix(code-review): nominate this repo's conventions from their tracked
sources") added exactly that file on the same branch. That claim is false as of
79fc0b9 and must not reach `notes/`. What follows is re-verified against the
manifest as it stands after `a28029f` ("fix(code-review): drop the
referencing_files rules from this repo's manifest").

**Mechanism claims, re-checked and still true:**

- Convention documents are read at the base ref, never the head under review.
  `source/toolkit/internal/review/manifest.go:330-335` (`Manifest.ConventionDocs`):
  a manifest's `conventions:` list **replaces** the built-in default list rather
  than extending it. The default list is `DefaultConventionDocs` at
  `source/toolkit/internal/reviewrun/prompt.go:92-98`: CLAUDE.md, AGENTS.md,
  .claude/CLAUDE.md, CONTEXT.md, CONTRIBUTING.md, docs/ARCHITECTURE.md,
  docs/CODE_STANDARDS.md.

- `readConventions` (`reviewrun/prompt.go:~112-135`) takes a `nominated bool`.
  A path from the *default* list that is missing at the base ref is skipped in
  silence (comment there: "A default that is not there is an ordinary answer").
  A path from a manifest's own `conventions:` list that is missing is appended
  to `missing` and surfaces as `MissingConventions` on the run result
  (`reviewrun/run.go:162` calls `readConventions(..., m.ConventionDocs(...),
  nominated)`; `run.go:191` threads `missing` through). This is the mechanism
  `.agents/code-review/manifest.yaml`'s own `conventions:` comment now leans on:
  nominating `CONTEXT.md` plus the five tracked `definitions/instructions/*.md`
  sources (rather than relying on `CLAUDE.md`/`AGENTS.md`, which are rendered
  output and untracked) converts a silent loss of coverage into a reported one.

- Escalation rules that read `referencing_files` are refused, not degraded, when
  the count is unavailable. `review/language.go:~305-309`: a file extension the
  build's language table does not list at all (as opposed to a listed
  extension whose language just has no extractor) makes the count unavailable
  by design — "the direction the table fails in matters more than its
  completeness". `review/condition.go:116` (`KeyReferencingFiles` description):
  "a rule reading an unavailable count is refused rather than read as low".
  Because this catalog ships arbitrary asset files inside definition bundles
  (formats outside the recognised set), one such file in a change would refuse
  the whole review if a `referencing_files` rule were present — which is why
  `.agents/code-review/manifest.yaml` (as of `a28029f`) carries no
  `referencing_files` escalation rule, keeping `changed_files` and `signals` as
  the raising criteria instead.

- `definitions/CONFIG-SCHEMA.md`'s review-manifest section still requires
  `version`, `reviewers`, `judge`, `validator`, `panels`, `defaults`; only
  `conventions:` and `exclude:` are optional. A repo cannot declare a bare
  `conventions:` override without declaring a full roster/panel set — which is
  why this repo's manifest (`.agents/code-review/manifest.yaml`) carries the
  full built-in roster verbatim (as `agtk code-review init` writes it) rather
  than a conventions-only file.

No note in the current index (37 notes, checked via INDEX.md before this task)
anchors to `review/manifest.go`, `reviewrun/prompt.go`, `review/language.go` or
`review/condition.go`, so there is nothing stale to reconcile here — this is a
new finding, not a re-verification of an existing note.
