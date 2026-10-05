---
about: "A settings key only Claude Code understands must carry platforms: [claude] on its definition; a stack cannot narrow it, and omitting it writes a stray table into codex config.toml"
saw:
  - definitions/settings/no-attribution.yaml
  - source/toolkit/internal/stack/parser.go
  - source/toolkit/internal/adapters/codex/config.go
  - source/toolkit/internal/cli/tests/no_attribution_stack_render_test.go
---

The codex adapter merges every settings definition that targets it into `.codex/config.toml`,
last stack wins per top-level key (`adapters/codex/config.go`, the comment above the settings
composition around line 425). It does not know which keys are Claude Code vocabulary, so the only
thing keeping a Claude-only key out is the definition's own `platforms: [claude]`
(`definitions/settings/no-attribution.yaml` carries it for `attribution`).

The narrowing cannot be moved up to the stack. `detectRepoOnlyFields` in `stack/parser.go`
rejects `platforms:` in a stack file with `repo_only_field` (`repoOnlyTopLevelKeys` is `platforms`
and `memory`), because both are properties of the consuming repo and belong to the entry
manifest. So a stack that lists a Claude-only setting is only as safe as that definition's own
`platforms`.

Testing it needs a second contributor. `stacks/no-attribution.yaml` lists only the claude-only setting, so
on its own it writes nothing to codex and `.codex/config.toml` does not exist at all; an assertion
that `attribution` is absent from a file that was never written proves nothing.
`no_attribution_stack_render_test.go` stages a fixture `shared` stack whose setting both platforms read
(`model: gpt-5-codex`), asserts `config.toml` exists and carries it, and only then asserts
`attribution` is absent.

Established by deleting `platforms: [claude]` from `no-attribution.yaml` and running
`go -C source/toolkit test ./internal/cli/tests -run NoAttribution`: the codex test failed with an
`[attribution]` table in `config.toml`; restoring the line made it pass. A render through
`--source/--stack` showed no `.codex/` directory even with both platforms requested, which is why
the test goes through an entry manifest instead.
