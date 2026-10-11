---
name: claude-only-settings-are-narrowed-on-the-definition
kind: gotcha
description: "A settings key only Claude Code understands must carry platforms: [claude] on its definition; a stack cannot narrow it, and omitting it writes a stray table into codex config.toml."
anchors:
  - path: definitions/settings/no-attribution.yaml
    blob: aa47e4a07a14
  - path: source/toolkit/internal/stack/parser.go
    blob: c85fae1c0eb7
  - path: source/toolkit/internal/adapters/codex/config.go
    blob: 26605d0b0b16
  - path: source/toolkit/internal/cli/tests/no_attribution_stack_render_test.go
    blob: bec5dd18835d
confidence: verified
---

The codex adapter merges every settings definition targeting it into `.codex/config.toml`
(`adapters/codex/config.go`) and does not know which keys are Claude vocabulary; the only thing
keeping one out is the definition's own `platforms: [claude]` (`definitions/settings/no-attribution.yaml:3`).

It cannot be narrowed at the stack: `detectRepoOnlyFields` rejects `platforms:` in a stack file with
`repo_only_field` (`repoOnlyTopLevelKeys`, `stack/parser.go:394`), since it belongs to the entry manifest.

Testing needs a second contributor: `stacks/no-attribution.yaml` alone writes nothing to codex, so
asserting `attribution` is absent from a file never written proves nothing.
`no_attribution_stack_render_test.go` stages a shared stack with `model: gpt-5-codex`, asserts
`config.toml` exists, then asserts `attribution` is absent. Deleting the `platforms` line made that
test fail with an `[attribution]` table.
