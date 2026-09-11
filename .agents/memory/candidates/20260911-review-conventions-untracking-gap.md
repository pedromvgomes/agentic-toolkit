---
about: no decision, plan or note records that untracking AGENTS.md leaves this repo's review conventions down to only CONTEXT.md
saw:
  - source/toolkit/internal/review/manifest.go
  - source/toolkit/internal/reviewrun/prompt.go
  - definitions/CONFIG-SCHEMA.md
  - .agentic-toolkit.yaml
---

Investigated for a PR that untracks `.agents/skills/`, `.codex/`, `AGENTS.md` (commit
5a66a2174280978d0efd896d307936ab18e312ad, "chore(git): untrack generated platform output").
A deep review flagged that this drops this repo's default review-convention sources
(`CLAUDE.md`, `AGENTS.md` are/become gitignored; only `CONTEXT.md` of the built-in
seven survives at the base ref) with nothing reported as missing.

Checked the memory index (37 notes) — none anchors to `review/manifest.go`,
`reviewrun/prompt.go`, or any convention-loading path; nothing to verify as stale.

1. **No ADR or note explains why agentic-toolkit runs on the built-in default review
   manifest rather than declaring `.agents/code-review/manifest.yaml`.** No such file
   exists in this repo (`find .agents -iname manifest.yaml` -> nothing). `git log --all
   -- .agents/code-review` is empty — one was never added and removed either. ADRs
   0007, 0009, 0011, 0015 and the v0.12.0 release notes describe the manifest/engine
   design generally but never discuss this repo's own choice not to adopt one.

2. Convention sourcing: `review/manifest.go:330-335` (`Manifest.ConventionDocs`) —
   a manifest's `conventions:` **replaces** the built-in default list, never extends
   it (doc comment there states the rationale). The default list lives at
   `reviewrun/prompt.go:92` (`DefaultConventionDocs`): CLAUDE.md, AGENTS.md,
   .claude/CLAUDE.md, CONTEXT.md, CONTRIBUTING.md, docs/ARCHITECTURE.md,
   docs/CODE_STANDARDS.md. Nothing in the codebase records the generated-vs-tracked
   tension for AGENTS.md specifically, or that untracking it degrades reviews — this
   PR's own commit message doesn't mention conventions either.

3. **No PR2/PR4 (or any multi-slice) plan for untracking `.agents/` is recorded
   anywhere** — not in `docs/releases/`, not under `.agents/`, not in git log
   (`git log --all --grep=untrack -i` shows only this one commit). The commit that
   untracks generated output is a single, already-applied commit with no forward
   reference to further slices or to the review-conventions gap.

4. Verified in `reviewrun/prompt.go:112-135` (`readConventions`): the function takes
   a `nominated bool`. A path from the *default* list that is missing at the base ref
   is silently skipped (comment: "A default that is not there is an ordinary
   answer"). A path from a manifest's own `conventions:` list that is missing is
   appended to `missing` and surfaced — `reviewrun/run.go:162` calls
   `readConventions(..., m.ConventionDocs(DefaultConventionDocs), nominated)` and
   `run.go:191` threads `missing` into `MissingConventions` on the result, which is
   reported. So a manifest that names its own list gets missing-file diagnostics;
   the built-in default list never does, by design — which is exactly the gap the
   review flagged: AGENTS.md going missing from the default list is invisible.

`definitions/CONFIG-SCHEMA.md` (~line 88-100) confirms the reviewer's premise:
`version`, `reviewers`, `judge`, `validator`, `panels`, `defaults` are all **yes**
(required) on the review manifest; only `conventions:` and `exclude:` are optional.
There is no schema path to declare `conventions:` alone without also declaring a full
roster/panel set.
