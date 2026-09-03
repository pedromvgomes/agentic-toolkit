---
about: hook `timeout:` is seconds, but SCHEMA.md documents it as milliseconds
saw:
  - definitions/SCHEMA.md
  - definitions/hooks/*.yaml
  - internal/adapters/claude/render.go
---

`definitions/SCHEMA.md` says the hook `timeout` field is "Timeout in milliseconds; 0 =
platform default". Claude Code reads `.claude/settings.json` hook timeouts as **seconds**,
and the adapter passes the value through unconverted — a rendered hook carries the same
integer the definition wrote.

Every hook definition in the repo already assumes seconds, which is what makes the doc the
odd one out rather than the definitions:

- `serena-claude-session-start.yaml: 3600` — installs `uv` and `serena-agent` over the
  network. 3600ms would be 3.6s, not enough to curl an installer; 1h is right.
- `rtk-claude-pre-tool-use.yaml: 90`, `serena-remind.yaml: 15`, `serena-cleanup.yaml: 90`.

Found by writing `memory-claude-session-start.yaml` with `timeout: 5000` — believing the
schema — and noticing the rendered `.claude/settings.json` said `5000`, unconverted, which
would have been a 83-minute timeout on a hook that runs `agtk memory stats`.

The schema text is generated from an `agtkdoc` tag, so the fix is in the struct comment
plus `go generate ./...`, not in SCHEMA.md. Left alone here because it would land a
regenerated schema doc in an unrelated PR — and see
[[generated-schema-docs-have-no-ci-guard]] for why that drift went unnoticed in the first
place.
