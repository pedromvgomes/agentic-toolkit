---
about: "plan-feature's whole rendered frontmatter is substring-scanned for Read, Grep and Glob, so those letters anywhere in it — a description word like \"Ready\" or a YAML comment — fail the allowlist test"
saw:
  - source/toolkit/internal/cli/tests/feature_flow_stack_render_test.go
  - definitions/commands/plan-feature.md
---

`TestPlanFeaturesToolAllowlistArrivesUnderTheKeyClaudeReads` takes
`strings.SplitN(cmd, "---", 3)[1]` of the rendered `.claude/commands/plan-feature.md` — the
entire frontmatter, not the `allowed-tools:` value — and fails on `strings.Contains` of `Read`,
`Grep` or `Glob`. It is case-sensitive and matches inside words, so `description:`, `requires:`
comments and `argument_hint` must avoid those capitalised substrings ("Ready", "Reads",
"Globally"). The failure message blames the allowlist, which misleads when the hit is prose.
