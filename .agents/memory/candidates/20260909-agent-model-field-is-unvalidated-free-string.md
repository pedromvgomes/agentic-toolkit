---
about: Agent.Model's agtkdoc list is descriptive only — nothing validates against it, so plan-reviewer's "fable" renders unchecked and is a deliberate choice, not an oversight
saw:
  - internal/definitions/types.go
  - internal/adapters/claude/files.go
  - definitions/agents/plan-reviewer/AGENT.md
  - docs/FEATURE-FLOW.md
  - internal/cli/tests/feature_flow_stack_render_test.go
---

`Agent.Model` carries an `agtkdoc` comment naming the shorthands, but the field is a plain
`string` with no parser-side or render-side check against them —
`internal/adapters/claude/files.go` passes `a.Model` straight into the rendered frontmatter with
no switch or validation. The same is true of the other three `Model string` fields in types.go.
The comment documents the common cases; it is not an authoritative enum, and any string
round-trips.

A list that reads like an enum and is not one invites the conclusion that a value absent from it
is a defect — which a review of this repository drew, filing `model: fable` as a RED. The tag now
names `fable` and says outright that the field is unvalidated, for that reason. `SCHEMA.md` is
generated from the tag, and nothing checks that it is current, so a tag edited without
regenerating goes stale silently.

`definitions/agents/plan-reviewer/AGENT.md` sets `model: fable`, which is none of the four
documented shorthands. This is intentional, not a typo: `docs/FEATURE-FLOW.md:39,64-66` and
`internal/cli/tests/feature_flow_stack_render_test.go:565-566` both establish that plan-reviewer
must run on a model different from whatever wrote the plan (opus, per FEATURE-FLOW.md:38), and
`fable` is that choice — the test asserts on the literal string `model: fable` in the rendered
agent file.
