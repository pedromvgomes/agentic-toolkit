---
name: hook-fail-closed-is-never-rendered
kind: gotcha
description: A hook's fail_closed parses and validates but no adapter ever writes it, so every rendered hook is fail-open on every platform.
anchors:
  - path: source/toolkit/internal/definitions/types.go
    blob: 1b36f7aa86a4
  - path: definitions/SCHEMA.md
    blob: d150b1d023d4
  - path: source/toolkit/internal/adapters/*/*.go
    matches:
      - path: source/toolkit/internal/adapters/claude/diff.go
        blob: a7daf0f1d7ce
      - path: source/toolkit/internal/adapters/claude/files.go
        blob: 859d771f3937
      - path: source/toolkit/internal/adapters/claude/instructions.go
        blob: b55d481270f8
      - path: source/toolkit/internal/adapters/claude/mcp.go
        blob: 69ecb724a03f
      - path: source/toolkit/internal/adapters/claude/render.go
        blob: 2aefeb6f4588
      - path: source/toolkit/internal/adapters/claude/settings.go
        blob: a76ce7243f96
      - path: source/toolkit/internal/adapters/codex/agent.go
        blob: 9e16b6f60b26
      - path: source/toolkit/internal/adapters/codex/agentsmd.go
        blob: 7da840baab61
      - path: source/toolkit/internal/adapters/codex/command.go
        blob: ce675e322186
      - path: source/toolkit/internal/adapters/codex/config.go
        blob: 721b4f9724a4
      - path: source/toolkit/internal/adapters/codex/files.go
        blob: d6830025416e
      - path: source/toolkit/internal/adapters/codex/render.go
        blob: 7180daae7ec6
      - path: source/toolkit/internal/adapters/fsops/fsops.go
        blob: 5d3b80df26ed
confidence: verified
---

`Hook.FailClosed` is declared at `source/toolkit/internal/definitions/types.go:289`
(`yaml:"fail_closed,omitempty"`) and documented at `definitions/SCHEMA.md:258` — "If true, a
non-zero exit blocks the action; default is fail-open."

No adapter renders it, and there are now two.

- **Claude.** `collectHooks` (`source/toolkit/internal/adapters/claude/settings.go:202`) is the
  only place a `Hook` becomes a Claude `hooks` entry, and it reads `h.Event`, `h.Matcher`,
  `h.Handler.{Type,Command,Prompt,Model}` and `h.Timeout` (`:225-226`) — never `h.FailClosed`.
  The drop is silent: nothing is reported.
- **Codex.** `collectHooks` (`source/toolkit/internal/adapters/codex/config.go:383`) drops it
  deliberately, and says so at `config.go:380-382`: "A Codex hook blocks by what it exits
  with, not by a key in the config, so there is nothing to write it to." Documented for
  consumers at `docs/CONSUMER-GUIDE.md:198` and
  `docs/adr/0015-a-stack-opts-into-a-platform-and-every-category-lands.md:87`.

Repo-wide the identifier appears only at that one declaration plus the Codex comment; the
string `fail_closed` otherwise appears only in the schemagen example
(`source/toolkit/internal/schemadoc/schemadoc.go:109`), the generated docs, a test fixture
(`source/toolkit/internal/definitions/tests/testdata/valid/definitions/hooks/log-tools.yaml:8`),
and `definitions/skills/open-pr/SKILL.md:68`.

So `fail_closed: true` parses cleanly, validates, and silently does nothing: the rendered
`.claude/settings.json` entry carries no field controlling it, and the platform's own
fail-open default governs. A hook that must block on failure has to encode that in the
command itself, not in this field. Codex is the reason this is unlikely to change by
accident — the canonical field has no destination there at all.

Related: [[nonzero-exit-needs-a-sentinel-in-execute]], on what a non-zero exit actually does.

The glob anchor is deliberate and spans *every* adapter directory
(`source/toolkit/internal/adapters/*/*.go`), not just `claude/`: the claim is that no file in
any adapter writes this field, which a newly added adapter would falsify invisibly to a
per-file or per-adapter anchor. `claude/` was the only adapter when this note was written,
and `codex/` appearing is exactly the event a narrower anchor would have missed.
