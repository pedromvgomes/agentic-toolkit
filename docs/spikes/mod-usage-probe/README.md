# Mod usage probe

Can a Claude Code mod (a plugin of function hooks) feed usage collection beside a collector that
reads transcripts? The mod in this directory records, per session, what `turn.step`, `turn.complete`,
`session.append` and `$.session.usage()` carry. It writes key names, ids and token counts only: no
message text and no credential. `compare.py` matches its figures against the transcript.

## Run

```bash
D=$PWD/docs/spikes/mod-usage-probe
claude plugin validate "$D"
USAGE_PROBE_OUT=/tmp/probe claude -p "Use the Agent tool once with subagent_type general-purpose and prompt 'reply with the word pong'. Then say done." \
  --plugin-dir "$D" --permission-mode acceptEdits --allowedTools Agent
python3 -I "$D/compare.py" /tmp/probe/usage-probe-<session>.jsonl \
  ~/.claude/projects/<mangled cwd>/<session>.jsonl ~/.claude/projects/<mangled cwd>/<session>/subagents
```

`USAGE_PROBE_KEY` set to a dummy value makes `session.end` send it as a bearer header to
`https://httpbin.org/headers` and record only whether it arrived intact.

## Findings

Measured in a cloud session, Claude Code 2.1.292, under `claude -p` (no terminal, no person to
enable hot reloading).

| Question | Finding |
|---|---|
| The four token counts | Present on `turn.step`'s result (`usage`, one per model request) and on `turn.complete` (`usage`, the sum of the turn's requests). Both carry `model`. A count the response left out reads as zero. |
| The message id | **Absent everywhere.** `turn.step`, its result, `turn.complete` and `session.append` (`door: response`) carry no `msg_…` id. The `session.append` row has `uuid`, the transcript row's uuid, not the API message id. The API types name a `message_id` elsewhere and say it is not the API id. |
| `session_id` | Available as `$.session.id()`. Not a field on any event. |
| Model name | Present (`turn.step` input and result, `turn.complete`). |
| Match with the transcript | Every `turn.step` usage matched exactly one deduplicated transcript assistant row by its four counts: 4 of 4 in the subagent session, 1 of 1 in the nested one, with no transcript row left over. `turn.complete` matches only a single-step turn. A multi-step turn carries the sum, which equals no transcript row. |
| Deriving `(session_id, message_id)` | Not possible from the mod alone. Matching a step to a row by counts is ambiguous when two messages report equal counts, which is the collision ADR 0025 rejects as a key. A mod would need the transcript to supply the id. |
| Subagent rows | Reach the mod. `turn.step`, `turn.complete` and `session.append` carry `agentId`, and the `agentId` equals the transcript's `agentId` (`isSidechain: true`). |
| `$.session.usage()` | Carries `context`, `rateLimits`, `startedAt` and `cost` (`usd`). It holds no token counts and no per-message data. Its `cost` is Claude Code's own estimate, not the stored-counts-times-price-table figure ADR 0025 computes. |
| Nested `claude -p` loads the mod | Only when told to: it loaded with `--plugin-dir` or `CLAUDE_CODE_PLUGIN_DIRS`, and did not load with neither. The nested session's `$.session.id()` was the id of the session the outer shell belongs to (its transcript's `sessionId` agrees), not a fresh id. |
| Loads in a cloud session and under `claude -p` | Yes, through `--plugin-dir` or `CLAUDE_CODE_PLUGIN_DIRS`. Hot reloading is not involved: it needs a person to answer a prompt, which `-p` cannot, and this probe never relied on it. |
| `$.http.fetch` | Reaches the public internet from a cloud session (`example.com`, `httpbin.org` returned 200). It ran in `session.end` under `-p` and completed. A header the mod read from an environment variable arrived at the server unchanged. |
| Credential it can present | Whatever the mod reads from the environment with `$.env.get` and puts in a header, so a per-user ingest key can ride it. `auth: handle` from `$.session.authorize()` sets the session's Anthropic credential and only for a first-party host, so it cannot authenticate to a Worker. |

## Not established

- Whether a Worker accepts the request. No Worker exists to post to; the fetch was proven against
  a public echo, not against ingest.
- Behaviour in an interactive cloud session or a terminal. Every measurement ran under `claude -p`.
- Whether `session.end` completes its fetch when the session is killed or the container is
  reclaimed. It shares one short bound with every other end hook, and the probe did not exceed it.
- Whether a mod can be placed in a cloud session without `--plugin-dir`: a plugin installed from a
  marketplace by the environment's setup script was not tried.
- Retry and ordering behaviour when a post fails mid-session. The probe posts nothing it needs to
  deliver.
- Rows from before the mod loaded, and a session whose model turn was interrupted before a usage
  figure arrived (`usage` is absent then).

## Recommendation

**Do not adopt a mod as a collector path. Use it, at most, for a live display.**

1. The mod cannot supply the idempotency key. ADR 0025 keys a row on `(session_id, message_id)`
   and rejects a hash of the figures. The mod has `session_id` but never the message id, so a row
   it posts either has no key or collides with the transcript collector's row for the same
   message.
2. A mod is a second place that has to be installed and enabled in every session, including each
   nested `claude -p`, which loads it only if told. A session without it is silently uncounted,
   while the transcript collector sees every session that wrote a file.
3. What the mod adds over transcripts is timeliness, which usage reporting does not need: the
   dashboard aggregates rows after the fact, and the transcript carries the same counts with the id.

**If a mod is added later**, it must not be an independent ingest path:

- Idempotency: it posts `(session_id, step usage)` as a hint, not a row, or it reads
  `message.id` from the session's transcript file before posting (the path a mod would use was not established), so a row is
  keyed `(session_id, message_id)` like the collector's and the Worker's upsert merges the two.
  Two writers with different figures replace each other under ADR 0025, so the mod must not
  post figures the transcript row does not contain.
- Credential: the same per-user ingest key as `docs/adr/0027-the-dashboard-knows-a-github-user-and-ingest-knows-a-key.md`,
  read with `$.env.get` and sent as a header. Nothing in the mod needs a second credential.
- Display: `$.session.usage()` and `turn.complete` give a status line or band the running cost
  without any ingest.
