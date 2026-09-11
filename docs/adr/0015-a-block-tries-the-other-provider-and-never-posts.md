# A block tries the panel's declared fallback, once, and never posts if it still fails

`agentic-driver` v0.7.0 lets a provider say, in `Result.Blocked`, that it declined to serve the
credential — an exhausted quota or a rejected token — rather than attempting the run and failing
at it. `agtk code-review` now reacts to that one outcome differently from every other unavailable
run: a panel every one of whose runs came back blocked is retried, whole, on the panel's own
declared `fallback:` (its twin on a different provider in the built-in manifest), and if the
review is still unavailable after that — whether no fallback was configured or the fallback was
blocked too — it reports no verdict to the caller and posts nothing to the pull request.

That last part is a deliberate, narrow carve-out from the review's own invariant that a failure
stays visible: ordinarily, a review that never reaches a verdict still posts, precisely so an
ordinary bug or outage is seen rather than silently repeating (a failing judge makes the whole
review unavailable; see `decide` in `source/toolkit/internal/reviewrun/run.go`). A block is different in kind —
a credential or quota condition, not a defect in the change or the review itself — and during a
provider-wide outage the visible-failure invariant would otherwise post the same non-finding to
every open pull request. So the carve-out is scoped to exactly the reviews that stayed
unavailable *because* of a block (`Review.Blocked`); an ordinary failure — a bad schema, a
sandbox refusal, a timeout — is never blocked and always posts, unchanged.

The retry is whole-panel and single-hop. Whole, because a panel's reviewers, judge and validator
are not decomposable today — a block that hits only the judge after every reviewer already
answered still re-runs the reviewers too, on the other provider, rather than inventing a
role-level retry. Single-hop, because the fallback attempt calls the review's internal, non-
retrying entry point directly rather than re-entering the fallback wrapper: even a manifest whose
`fallback:` pointers formed a cycle costs exactly one extra panel, with no cycle-detection
bookkeeping needed. Today's two-provider reality (`claudecode`/`codex`) needs nothing deeper.

## Considered options

**Any unavailable review skips posting, not just a blocked one.** Rejected: it would remove the
visibility ordinary failures were made to keep. A schema bug or a sandbox misconfiguration is
exactly the kind of failure a person on the pull request should see, and blocking is the one
outcome the driver itself distinguishes as safe to route around silently.

**Chain fallbacks across more than one hop, with cycle detection.** Rejected for now: there are
two providers, so a chain past one hop has nothing to reach. Adding the bookkeeping ahead of a
third provider existing would be speculative.

**Retry only the blocked role (reviewer, judge or validator) rather than the whole panel.**
Rejected: panels are not decomposable that finely in the manifest today, and building that
decomposition costs real design for a case — a block that lands after reviewers already
answered — that is comparatively rare. Re-spending the reviewers is the accepted cost of keeping
one code path.

## Consequences

- `source/toolkit/internal/review`'s `Panel` gains an explicit `fallback:` field, validated like
  `escalate[].to` (must name a declared, different panel). It is not inferred from the
  `-codex`-suffix naming convention the built-in manifest happens to use, so a manifest that
  names its panels differently still works.
- A fallback is refused at parse time if it costs less (fewer reviewers × quorum) than the
  panel declaring it. Equal cost is the floor, not the ceiling: a panel may fall back to one
  at least as deep as itself, but not to a shallower one. Without this, a panel an escalation
  rule raised (auth, crypto, a wide blast radius) could silently give up that depth the moment
  its provider was rate-limited — the retry would run, answer, and read as the review the rule
  asked for while spending a fraction of it.
- The predicate for "this review's failure was a block" requires every run that did not answer
  to have been blocked, not merely that some run was. A panel with more than one run can have
  one instance blocked while a sibling instance answers and a later run (the judge, say) fails
  for an unrelated, ordinary reason; that review's unavailability has a cause a different
  provider cannot fix, and a block elsewhere in the same panel must not make it invisible.
- A fallback attempt reuses the first attempt's review root, diff and convention reads rather
  than rebuilding them — only the panel differs between the two, and neither the tree nor the
  manifest changed in between. The runs and cost the first, blocked panel already made are
  carried into the returned `Review` rather than discarded with it.
- A `fallback:` is refused, too, if it shares any provider with the panel declaring it: "on a
  different provider" is enforced, not merely a naming convention a manifest is trusted to
  follow. `standard.fallback: deep` (same provider, just deeper) is otherwise a perfectly legal
  fallback by every other check, and would recur into the identical block the moment it
  actually mattered.
- A prompt-injection finding (`security:prompt-injection`) the first, blocked panel's reviewers
  already caught is carried into the fallback's own result if the fallback panel does not
  independently reach the same finding — matched by fingerprint, since ids are per-run and mean
  nothing across two different panels. ADR 0008's reattachment happens inside one judge's own
  run, and a judge that never starts (blocked, same as any other outage) never reaches it;
  without this, the retry this feature exists to make is the one path that can convert a real,
  already-detected injection into a review that looks clean.
- Only `claudecode`'s driver dialect can report a block as of v0.7.0; `codex`'s explicitly
  reports none. A `codex`-backed panel that hits a rate limit today fails ordinarily (ordinary
  failure, posts as before) rather than triggering a fallback — the mechanism is ready for the
  day `codex`'s dialect can read one, and does nothing dishonest in the meantime.
