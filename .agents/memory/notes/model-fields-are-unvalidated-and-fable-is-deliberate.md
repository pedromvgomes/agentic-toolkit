---
name: model-fields-are-unvalidated-and-fable-is-deliberate
kind: gotcha
description: "The model shorthand list in agtkdoc is documentation, not an enum — nothing validates it, and plan-reviewer's `model: fable` is a deliberate choice a review has already misfiled as a defect."
anchors:
  - path: internal/definitions/types.go
    blob: ee984767cd43
  - path: internal/adapters/claude/files.go
    blob: ee7d951bccd8
  - path: definitions/agents/plan-reviewer/AGENT.md
    blob: 765958b9c537
  - path: docs/FEATURE-FLOW.md
    blob: afaffdeee0ff
  - path: internal/cli/tests/feature_flow_stack_render_test.go
    blob: c4ac32dcaf37
confidence: verified
---

`Agent.Model` (`internal/definitions/types.go:181`) is a plain `string` whose `agtkdoc` tag
names the shorthands. Nothing checks a value against them: the renderer passes `a.Model`
straight into the frontmatter (`internal/adapters/claude/files.go:225`), and no non-test file
in `internal/` contains the literal `"sonnet"` — the shorthands appear in tests only. The
other three `Model string` fields in `types.go` (`:244`, `:284`, `:324`) are the same.

A list that reads like an enum and is not one invites the conclusion that a value absent from
it is a defect, and a review of this repository drew exactly that conclusion, filing
`model: fable` as a RED. The tag now names `fable` and says outright `Not validated: an
unknown value is rendered as written.`, for that reason. `SCHEMA.md` is generated from the
tag and nothing verifies it is current — see [[generated-schema-docs-have-no-ci-guard]] — so a
tag edited without regenerating goes stale silently.

`definitions/agents/plan-reviewer/AGENT.md:6` sets `model: fable`. This is intentional:
`docs/FEATURE-FLOW.md:39` records it in the per-role model table and `:64` states the reason —
the plan is drafted on opus (`:38`) and handed to a reviewer running on *a model that did not
write it*. `internal/cli/tests/feature_flow_stack_render_test.go:625` asserts on the literal
string `model: fable` in the rendered agent file, so changing it fails a test rather than
silently collapsing the two-model split.
