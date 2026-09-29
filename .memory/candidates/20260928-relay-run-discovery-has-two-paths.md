---
about: internal/relay.Await finds the run it dispatched two different ways depending on what GitHub's dispatch response gave it, and the fallback path depends on an exact string match against the relay workflow's run-name in a sibling repository
saw:
  - source/toolkit/internal/relay/relay.go
  - source/toolkit/internal/relay/await.go
  - docs/adr/0020-a-relay-posts-as-the-app-from-a-separate-repository.md
---

`Dispatch` (`relay.go`) always sends `return_run_details: true` on the `workflow_dispatch` POST.
When GitHub answers with a run id (the common case, per GitHub's 2026-02-19 changelog), `Await`
reads that run directly by id — `readRun` in `await.go` takes the `id != 0` branch and never lists
runs at all.

The fallback exists because GitHub has changed this endpoint's behavior before and reverted it
(cited in the package doc as community discussion #182272, Dec 2025): when the dispatch answers a
plain 204 with no run id, `Await` instead lists the workflow's most recent runs and matches one by
`display_title == runName(req)` (the string `"<action> <repo>#<pr>"`, e.g. `"run acme/widgets#42"`)
and a `since` cutoff (truncated to the second, because GitHub reports `created_at` at one-second
resolution).

That match depends on the *other* repository — `pedromvgomes/agentic-toolkit-code-review-relay`'s
own `.github/workflows/relay.yml` — carrying a `run-name:` line that renders to exactly that same
string: `run-name: ${{ inputs.action }} ${{ inputs.repo }}#${{ inputs.pr }}`. Nothing in this repo's
own tests can catch a mismatch, since the two live in different repositories; `runName`'s own doc
comment in `await.go` records the expected format as the contract, and a relay whose `run-name:`
render differs from it is only ever found through the id GitHub names directly — the fallback path
matches nothing, and `Await` runs out its timeout.
