---
about: "multi-provider" already exists per-reviewer in code review, and is unrelated to source/toolkit/internal/stack rendering, which is single-adapter (Claude only) and has no Codex platform
saw:
  - source/toolkit/internal/review/manifest.go
  - source/toolkit/internal/review/parse.go
  - source/toolkit/internal/provider/provider.go
  - source/toolkit/internal/stack/types.go
  - source/toolkit/internal/cli/render.go
  - source/toolkit/internal/adapters/claude/render.go
  - source/toolkit/internal/definitions/types.go
  - CONTEXT.md
  - docs/adr/0007-untrusted-heads-are-closed-structurally.md
---

A "stack" (`source/toolkit/internal/stack`, doc comment at `source/toolkit/internal/stack/types.go:1-33`) is not a PR/branch
stack. It is a manifest of definitions (skills/agents/rules/instructions/commands/hooks/mcp/
settings) that `source/toolkit/internal/resolver` resolves and `source/toolkit/internal/adapters/claude` renders into Claude
Code's on-disk layout (`.claude/` + `CLAUDE.md`). `source/toolkit/internal/cli/render.go:8,68-84` imports
`source/toolkit/internal/adapters/claude` directly and calls `claude.Render(plan, opts)` unconditionally —
there is no platform/provider selection at this layer at all, and `source/toolkit/internal/adapters` has only
one subpackage, `claude` (confirmed by `find source/toolkit/internal/adapters -maxdepth 2 -type d`).

`source/toolkit/internal/definitions/types.go:71-89` declares a `Platform` enum (`claude`, `cursor`, `copilot`,
`opencode`, `agents`) used only to narrow which `extensions.<platform>` block a definition may
carry (`source/toolkit/internal/definitions/parser.go` `validateExtensionsAgainstPlatforms`/
`presentExtensions`, see note `platform-extension-check-is-a-hand-written-switch`). Nothing in
`source/toolkit/internal/resolver` or `source/toolkit/internal/adapters/claude` reads `Platform` (`grep -rn Platform
source/toolkit/internal/resolver source/toolkit/internal/adapters/claude` -> 0 hits): the allowlist is validated at parse time
and then never consulted by rendering. There is no `codex` entry in this enum and no Codex
adapter under `source/toolkit/internal/adapters`.

Multi-provider support for *reviews* already exists and is unrelated to any of the above.
`source/toolkit/internal/provider/provider.go:32` lists `Names = []string{"claudecode", "codex"}`, and every
Reviewer/Judge/Validator ("Runner") in a review manifest carries its own required `provider:`
field (`source/toolkit/internal/review/manifest.go:105`, enforced non-empty at `source/toolkit/internal/review/parse.go:232`).
CONTEXT.md's glossary states the opt-in mixing explicitly: "a repo reviewing locally with one
**Provider** and its pull requests with another could say so for its reviewers and not for its
judge" (CONTEXT.md:153-155, Panel entry) — i.e. a single panel can already run some reviewers
through `claudecode` and others through `codex` simultaneously; no new mechanism is needed for
that. `docs/adr/0007-untrusted-heads-are-closed-structurally.md` documents in detail how the two
providers already differ for review sandboxing (Claude Code accepts `--setting-sources ""` and a
per-tool allowlist; Codex has neither, discovers `AGENTS.md`/`.codex/config.toml` unconditionally
and admits only a sandbox mode via `PermissionArgs`).

So a task framed as "stack rendering needs to support multiple providers simultaneously for
review" is conflating two systems: review-panel multi-provider is already shipped and opt-in
per-reviewer; `source/toolkit/internal/stack`/`render.go` multi-provider (e.g. adding a Codex definitions
adapter alongside `source/toolkit/internal/adapters/claude`) is a distinct, unstarted piece of work with no
dispatch mechanism, no Codex `Platform` constant, and no config key wiring `render` to a choice
of adapter.
