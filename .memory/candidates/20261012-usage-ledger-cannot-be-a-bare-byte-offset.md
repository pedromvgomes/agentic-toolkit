---
about: "A usage ledger that stores only a byte offset is unsafe: ParseFile always reads from byte 0, holds back the file's last message, and a message's records need not be adjacent; ADR 0025 makes re-sending idempotent so the ledger is an optimisation, not a correctness device"
saw:
  - source/toolkit/internal/usage/parse.go
  - source/toolkit/internal/usage/session.go
  - docs/adr/0025-usage-data-lives-in-d1-behind-a-worker-and-cost-is-computed-at-query-time.md
  - CONTEXT.md
---

Evidence (read, not run):
- `ParseFile` (`usage/parse.go`) takes only a path, opens it and reads from the start; no offset/resume parameter exists. `grep -rn -i 'ledger\|offset' docs/adr docs/spikes CONTEXT.md` -> 0 hits.
- A streamed message is several records with one `message.id`, "not necessarily adjacent"; the row comes from the last usable record. A message is complete if any record has stop_reason or a different assistant id follows. The final assistant message with no stop_reason goes to `Result.Held`, not `Rows`. An offset taken at EOF after such a parse skips the held message's later records forever; an offset taken mid-message ships a row with partial counts. Safe watermark = byte offset of the first record of the Held message (EOF if none held), which requires the parser to report record offsets (it does not today).
- Row identity is `(session_id, message_id)`, ADR 0025; ingest is an upsert, later write replaces earlier. So re-sending a row is harmless if figures are identical; the ledger's job is bandwidth/progress display, and a double flush duplicating rows is already safe server-side. Still the replace rule means re-sending a partial row after a complete one would regress it.
- `ParseSession` (`usage/session.go`) reads main + `<session>/subagents/agent-*.jsonl` separately, so positions are per file. Subagent rows are Row.IsSidechain and ids sit in the same (session_id, message_id) space.
- Identity is stamped by the parsing process's env (see sibling candidates), so a resumed parse over a partial file stamps the same.
- CONTEXT.md bans "upload", "sync", "import", "push" as synonyms for **Ingest** (Usage section), and "uploader" for **Collector**; issue #149 names the command `agtk usage push`, which collides with that _Avoid_ list. No CONTEXT.md term exists for ledger/queue.
- Goreleaser builds only darwin+linux (`.goreleaser.yaml` goos), so Windows is not a supported target; no cross-process lock exists anywhere in `source/toolkit/internal` (grep flock|LockFileEx|O_EXCL: only `memory/hits.go:225` O_EXCL for unique shard files and temp+rename atomic writes). `updatestate` writes with plain os.WriteFile, not atomic, no lock.
