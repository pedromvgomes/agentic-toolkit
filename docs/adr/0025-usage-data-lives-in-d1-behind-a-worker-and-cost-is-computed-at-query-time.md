# Usage data lives in D1 behind a Worker, and cost is computed at query time

Usage tracking has to collect token counts from local terminal sessions and from cloud sessions whose
container is reclaimed when the session ends, and serve a dashboard that drills down by project,
skill, repo and Claude account. Where those records are stored decides who can write them, how they
are aggregated, and whether a change to a price rewrites history.

## Decision

**Records are stored in a Cloudflare D1 database, written and read through a Cloudflare Worker.** The
Worker accepts ingest requests from collectors, serves the query endpoints, and serves the dashboard.
No session holds a database credential or a credential to any repository: a session presents an
ingest credential to the Worker and nothing else. Which credential that is belongs to its own
decision.

**Raw token counts are stored per model; dollars are derived at query time.** Each row keeps the
`usage` figures Claude Code records for an assistant message (input, output, cache-creation and
cache-read tokens) with the model that produced them. A price table maps a model to a rate per token
kind, and a query multiplies counts by the rate in force for the row's model and timestamp. The
stored rows hold no dollar amount, so correcting a price, adding a model or adding a price effective
date changes every total, past and future, without rewriting a row. A model with no entry in the
price table is reported as unpriced, with its token counts, rather than costed at zero.

**A row is identified by `(session_id, message_id)`.** `session_id` is the Claude Code session that
wrote the transcript row and `message_id` is the id of the assistant message in `message.id`. Ingest
is an upsert on that pair, so a collector that re-reads a transcript, retries a request after a
timeout, or runs twice over one session adds nothing. Two ingests of the same row with different
figures are not merged; the later one replaces the earlier.

**The collector makes no model calls.** It reads transcripts and link records, posts rows, and
holds no model credential. It is deterministic and sits on the path of hooks and CI the same way the
rest of the binary does (`docs/adr/0002-no-model-calls-in-agtk.md`). Attribution is computed from
those rows as `docs/adr/0026-skill-cost-is-attributed-through-span-propagation-across-sessions.md`
describes; this decision stores what attribution needs and does not choose how it is resolved.

## Considered options

**A git repository of per-session JSONL files.** Rejected. A cloud session would need push access to
a data repository, which means a write credential inside a container that also runs untrusted
repository code, and one more place a token can leak from. Concurrent sessions push to the same
branch, so every ingest is a rebase-and-retry race, and the sessions that run in parallel are the
ones that produce the most rows. A drill-down is an aggregation across every file, and a dashboard
would have to read and parse the whole history on each load or maintain a second store to avoid it.

**Git as the source of truth, with D1 derived from it.** Rejected. It keeps every cost of the first
option, because sessions still have to write to the repository, and adds a sync from the repository
into D1 that has to be kept correct and can lag. The history that git would preserve has no reader
that D1 does not serve: nothing in the epic reconstructs a past state of the usage data.

**Store dollars at ingest.** Rejected. The price in force when a row arrives is baked into it, a
wrong or missing price is a permanent error in the stored data, and a new model's rows are costed at
whatever the collector's price table said that day. Token counts are the observation; dollars are an
interpretation of it, and the interpretation has to stay changeable.

**A hash of the usage figures as the key.** Rejected. Two distinct messages that report equal counts
collide, and the second is silently dropped.

## Consequences

- One more deployable to own: a Worker and a D1 database with their own deploy, migrations, secrets
  and failure modes, none of which the rest of the repository has.
- Drill-downs are SQL aggregations over indexed columns. The query layer, not the dashboard,
  does the grouping, and totals reconcile because every view sums the same rows.
- No session needs push access to a data repository. A cloud session's only outbound write is a
  request to the Worker, authenticated by the ingest credential.
- Cost is only as complete as the price table. A model released before its price is entered shows as
  unpriced until it is added, and then every total that includes it changes.
- Ingest is safe to retry and safe to run from more than one place, so a collector can re-send
  without tracking what it already sent.
- ADR 0002 holds. The collector and the ingest path call no model.
