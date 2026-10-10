---
about: "no code in the repo reads Claude Code transcripts; ADRs 0025/0026 and CONTEXT.md Usage section already fix the usage-row contract"
saw:
  - docs/adr/0002-no-model-calls-in-agtk.md
  - docs/adr/0025-usage-data-lives-in-d1-behind-a-worker-and-cost-is-computed-at-query-time.md
  - docs/adr/0026-skill-cost-is-attributed-through-span-propagation-across-sessions.md
  - CONTEXT.md
  - source/toolkit/internal/version/version.go
  - docs/spikes/mod-usage-probe/README.md
---

Checked for issue #146 (transcript reader).

- `grep -rnE "\.jsonl|transcript_path|\.claude/projects" source/toolkit --include=*.go` -> only memory's `.hits.jsonl` (internal/memory/types.go:47); curator, reviewrun and cli/status.go never read session transcripts. No parser to reuse.
- Row identity is `(session_id, message_id)` with `message_id` = `message.id` (ADR 0025); the mod-usage-probe README (line 33) says matching to usage required a *deduplicated* transcript, i.e. a message id can appear on more than one transcript row, so a reader must dedupe per message.id.
- Subagent transcripts live at `<session>/subagents/agent-<id>.jsonl`, share the parent's `sessionId`/`promptId` (ADR 0026).
- Version: `internal/version.Current()` (version.go) is importable by any internal package; unstamped test build returns "dev" unless debug.ReadBuildInfo has a real Main.Version.
- Guard tests in internal/cli/tests/credential_surface_test.go walk all of internal/ for `"APPROVE"` and repo write-endpoint strings; a new package is covered automatically by those, not by the hand-listed slices.
- CONTEXT.md Usage section (~line 680-760) already defines Usage row, Span, Collector, Ingest, Claude account, Link record; it has no Transcript/Session/Sidechain entry.
