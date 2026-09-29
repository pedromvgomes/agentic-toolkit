# A missing provider tries the panel's fallback, and still posts if the review stays unavailable

ADR 0015 lets a **Block** retry the panel on its declared `fallback:` and keeps a review that is
still blocked off the pull request. A provider whose CLI is not installed on the machine running
the review is a second condition a different provider can fix, so a run that could not be started
for that reason — `agentic.New` failing to find the binary on `PATH`, or `Driver.Ready` finding it
absent, empty, a directory or not executable — now also counts toward trying the fallback. A panel
whose unanswered runs are all blocked or missing retries once, whole, on its twin, exactly as ADR
0015 describes. It does not share the Block's silence: a review that is still unavailable after
the fallback, or whose panel has no fallback, posts its "no verdict" like any other outage.

The two halves are kept apart because they answer different questions. Falling back asks "could
another provider have answered?", and for a missing binary it could. Staying silent asks "is this
something the pull request's readers must not be spammed with?", and ADR 0015 grants that only to a
credential or quota condition, which during a provider-wide outage would otherwise post the same
non-finding to every open pull request. A missing binary is local to one machine's setup, and
unless it shows up somewhere, somebody keeps running reviews that never review anything.

Recognising the condition by where the error arose, rather than by its type, is deliberate.
`agentic-driver` wraps many failures in `ErrProviderUnavailable`: the missing binary, and also
timeouts, cancellations, a crashed CLI, an unreadable event stream and a failed schema write. Every
failure except the first comes from a provider that started, and ADR 0015 already keeps those
ordinary. So only the error from constructing the driver or from its readiness check, both of which
run before anything is started, marks a run as a **Missing provider**.

## Considered options

- **Count a missing provider as a Block.** This was the smallest change, but it would make a review
  on a machine without the CLI silent whenever the fallback also failed or no fallback existed. The
  fallback header would also describe a missing binary as a declined credential.
- **Match `errors.Is(err, agentic.ErrProviderUnavailable)` wherever a run fails.** This was rejected
  because it would route timeouts, cancellations and crashes around to the other provider, which is
  the ordinary failure ADR 0015 keeps visible.
