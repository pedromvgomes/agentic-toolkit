---
about: the 24h update-check throttle bounds successful checks only — a failed or unfinished check leaves state unwritten, so the next invocation retries
saw:
  - internal/updatecheck/checker.go
  - internal/updatecheck/throttle.go
  - internal/updatestate/state.go
  - internal/cli/root.go
---

Kind: **gotcha**.

`ShouldCheck` (internal/updatecheck/throttle.go:50) gates on
`g.Now.Sub(g.State.LastUpdateCheck) < interval`, which reads like "at most one network call
per 24h". It is not. `LastUpdateCheck` is only ever written from one place, and only after
the call succeeds:

- `Checker.run` returns early on provider error (internal/updatecheck/checker.go:59-62),
  **before** the `updatestate.SaveTo` at checker.go:68-73. An offline machine, a 5xx from
  GitHub, a rate-limit 403, or the 2s context timeout (checker.go:53-57) all take that path.
  Nothing is persisted, so `LastUpdateCheck` stays zero and the very next `agtk` invocation
  spawns another goroutine and tries again.
- The write also loses to process exit. `startBackgroundCheck` spawns the goroutine in
  `PersistentPreRunE` (root.go:208) and `drainBackgroundCheck` does a **non-blocking**
  select in `PersistentPostRunE` (root.go:225-236, `default: return`). A command that
  finishes faster than the HTTP round-trip exits while the goroutine is still in flight;
  the goroutine dies with the process, so neither the banner prints nor the state file is
  written.

Consequence for anyone reasoning about network behaviour or writing a test: for fast
commands on a slow or failing network, agtk issues a GitHub API call on *every single
invocation* and never persists a throttle stamp — the opposite of what the interval
suggests. The rate limit that actually protects the user is GitHub's unauthenticated
60 req/hr/IP, noted at internal/updatecheck/provider.go:44-45, not this interval.

Two further asymmetries in the same area:

- `agtk update` passes `checker.Start("")` (internal/cli/update.go:80), and `statePath == ""`
  skips persistence entirely (checker.go:68). An explicit `agtk update` therefore never
  advances the background throttle.
- `Checker.Result` is 1-buffered and always closed (checker.go:52, 34), so the non-blocking
  select is safe, but a result that arrives after `PersistentPostRunE` has run is simply
  dropped — the update banner is best-effort, never guaranteed.
