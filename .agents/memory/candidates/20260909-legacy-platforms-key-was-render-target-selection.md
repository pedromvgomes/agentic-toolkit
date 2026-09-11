---
about: v1's guarded top-level `platforms:` key meant the same thing a v2 stack-level platform-targeting field would mean, and nothing but the hand-maintained legacyTopLevelKeys list stands in the way of reusing the name
saw:
  - internal/stack/parser.go
  - docs/MIGRATION.md
  - internal/definitions/types.go
  - internal/stack/tests/parser_test.go
targets: stack-schema-is-strict-and-legacy-keys-are-intercepted
verdict: still-true
---

Re-checked the target note's claim that `legacyTopLevelKeys` (`internal/stack/parser.go:372`)
guards `platforms` alongside `source`, `presets`, `externals`, `definitions` — still true,
same line, unchanged.

New finding not in the target note: what v1's top-level `platforms:` actually configured, and
whether it's safe to hand the name back to a v2 field.

Pre-collapse commit `9eccf31~1` (before "feat(stack)!: collapse consumer config + preset into
unified stack manifest (v2)") had `internal/config/types.go:19`:
`Platforms []definitions.Platform \`yaml:"platforms,omitempty" agtkdoc:"Render only for these
platforms. Empty means render every platform supported by each definition."\`` — i.e. exactly
"which rendering targets to render for," opt-out via omission. Values were the same enum still
in `internal/definitions/types.go:76-81` today (`claude`, `cursor`, `copilot`, `opencode`,
`agents`, and now also `codex`).

`docs/MIGRATION.md:125-127` documents the removal: "`platforms:` is no longer a config field.
Platform targeting is applied at render time per command (future work — currently agtk renders
for all platforms each definition supports)." So the v1 concept is not CI/OS platforms — it is
the identical concept to "a list of target rendering platforms," explicitly deferred as future
work, not retired as a mistake.

`legacyTopLevelKeys` (`internal/stack/parser.go:372`) is a pure string-match guard
(`detectLegacyConfig`, `:359-370`) with no semantic linkage to the struct — it exists only to
turn a v1 leftover key into a migration-hint error instead of a confusing "unknown field"
error. Nothing in the guard or in `stack.Stack` ties the name to any specific meaning, so
reusing `platforms` as a new v2 field name is not blocked by any invariant — only by this list.

Practical safety check: `TestParseBytes_LegacyConfig_Rejected`
(`internal/stack/tests/parser_test.go:85-96`) only exercises `source`+`presets` together; no
test pins `platforms` specifically as guarded, so removing it from the list breaks nothing in
the suite. A real un-migrated v1 file always also carries `source:` (required in v1's schema
per the old `internal/config/types.go` doc comment "Slice-1 scope: source, platforms,
externals, presets"), which stays guarded — so dropping `platforms` alone from
`legacyTopLevelKeys` does not meaningfully weaken migration detection in practice.

One doc that would go stale if `platforms:` returns as a v2 field: `docs/MIGRATION.md:125-127`
currently asserts it "is no longer a config field," which becomes false.
