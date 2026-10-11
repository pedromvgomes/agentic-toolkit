---
about: "Claude Code transcripts are read only by internal/usage; the row contract (ADRs 0025/0026, CONTEXT.md Usage) is fixed elsewhere, and no other package opens session transcripts"
saw:
  - source/toolkit/internal/usage/parse.go
  - source/toolkit/internal/usage/session.go
  - docs/adr/0025-usage-data-lives-in-d1-behind-a-worker-and-cost-is-computed-at-query-time.md
  - docs/adr/0026-skill-cost-is-attributed-through-span-propagation-across-sessions.md
  - source/toolkit/internal/cli/tests/credential_surface_test.go
---

- `ParseFile` and `ParseSession` in `internal/usage` are the only readers of session transcripts; the curator, `reviewrun` and `cli/status.go` never open one. Row identity is `(session_id, message_id)` (ADR 0025), and subagent transcripts at `<session>/subagents/agent-*.jsonl` carry the parent's `sessionId` (ADR 0026), which is why `ParseSession` can concatenate them with the main file.
- The guard tests in `internal/cli/tests/credential_surface_test.go` walk all of `internal/`, so a new package is covered without being added to a hand-listed slice.
- `internal/usage` makes no model call (ADR 0002) and never reads the clock: the same file always yields the same rows.
