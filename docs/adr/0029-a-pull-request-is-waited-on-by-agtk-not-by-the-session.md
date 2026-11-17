# A pull request is waited on by agtk, not by the session

`implement-handoff` does not stop at an open pull request. Step 7 watches it until its checks,
lydite's verdict and its review threads say it is mergeable, fixes on the branch whatever fails
or appears, and gives up after 2 hours of wall-clock since the latest push. Most of that phase is
waiting: CI takes minutes to tens of minutes per push, and nothing can be done until it reports.

A session that waits by polling pays for every poll. Each one wakes the model, which reads the
whole conversation again to decide that nothing has changed: at one poll every 5 minutes, a wait
that runs its cap is two dozen turns over a context already holding the plan, every task's
review and the pull request's review loop. The decision each poll makes is not a judgement. It is
a fixed rule over GitHub's record of the head — and a fixed rule belongs in `agtk`, tested, rather
than in prose a model re-reads and re-applies each time.

## Decision

**The session hands the wait to `agtk pr wait owner/repo#N`.** It runs the command in the
background and is woken once, when the command exits; the exit code is the verdict, and `--json`
names what it found. The command polls every `--poll` (5 minutes) and ends at the first poll that
reaches a verdict or when `--cap` (2 hours) passes with something still pending. While it waits,
no model turn is spent.

**The head's check runs and commit statuses decide.** One poll reads the pull request (its head
and state), the check runs and the combined commit status of that head, the conversation's
comments and the review threads. A check run that completed with anything but success, neutral
or skipped, or a status that is failure, error or a state GitHub does not document, needs
action. A check run not completed, or a status pending, keeps the wait going — except
`lydite/referral`, the one status allowed to stay pending: it waits for a human to clear a
referral, not for anything still running, and a clean verdict reports it.

**Only the newest workflow run decides.** Every `pull_request` event, an edit of the description
included, starts a new GitHub Actions workflow run on the same head, each a check suite of its
own, and a run the workflow's `concurrency` cancels leaves its cancelled and failed check runs on
the head beside the newer run's, often under other names (a matrix job cancelled before it
expanded carries the unexpanded expression). Check runs are deduplicated by name only within a
suite, so the poll also reads the head's workflow runs and judges only the check runs of the
newest run of each workflow and triggering event: the latest created, the higher run ID breaking
a tie. A newest run with no check runs on the head, still queued or failed before any job
started, is judged by its own status and conclusion. A check run in a suite no workflow run owns,
from any App other than GitHub Actions, is judged as it is. A token GitHub refuses the
workflow runs, one without `actions: read`, reads no runs, so every check run is judged as it is
and the wait polls as usual.

**lydite's results comment is detail, never a verdict.** lydite reports per head through the
head's checks and statuses. Its `<!-- lydite:results -->` comment is rewritten on each push but
names neither the head it describes nor when that head was pushed, so it cannot say whether it is
current. The command reports its failed and referred rows beside an action-needed verdict or a
clean one with a referral pending, and marks them possibly stale when it saw lydite still running
on the head after the comment was last written. Only a comment whose author is exactly the lydite
App's bot account, `<slug>[bot]` compared without case with the slug from `--lydite-app`
(`lydite` by default), is read: any other App, or `github-actions[bot]` from any workflow, can
post the marker, and its rows would reach a session that edits code as findings to fix.

**A review thread waits unless the token's own account spoke last or lydite wrote it alone.** A
resolved thread is done. An unresolved one is answered when its latest comment was written by the
account the token belongs to, and waiting otherwise, outdated or not; one whose latest author is a
deleted account is waiting, since nobody can say it was answered. An unresolved thread whose
opening comment and latest comment are both by the lydite App's bot account, the same
`<slug>[bot]` the results comment is read from, is a surviving mutant and neither waits nor counts
as answered: lydite fails its own check while the mutant survives and deletes the comment once it
is killed, so that check carries the verdict, pending while lydite runs and action needed once it
fails. A thread any other account opened or spoke last on is judged as any other. Answered threads
are counted beside the verdict and never hold the wait: only a person resolves a thread, so
counting them would make every pull request with an answered conversation need action on every
poll.

**No checks is a verdict.** A head with no check runs and no statuses on the first poll is
pending, because CI registers its checks a moment after a push. Still bare on the second poll, it
has no CI that is going to report on it, and the wait ends rather than spending the cap on
nothing.

**A moved head ends the wait.** The first poll records the head; a later poll that finds another
ends the wait as head moved. The caller runs it again for the new head, with a fresh cap. That is
how every push restarts the 2-hour clock: the session's own pushes are each followed by a new
run, and somebody else's push surfaces as exit 4 and a new run.

**A read failure is retried, three times.** A poll whose reads fail decides nothing and the next
poll tries again. Three consecutive failed polls end the wait as could-not-read: an outage long
enough to cover a quarter of an hour is reported, not waited out to the cap.

**The exit code is the contract.**

| Code | Verdict |
|---|---|
| 0 | clean: every check run passed, every status succeeded but `lydite/referral`, no thread waiting |
| 1 | the command did not start: a malformed argument or flag |
| 2 | action needed: a check run or status failed, or a thread is waiting; each is named |
| 3 | cap reached with something still pending; each is named |
| 4 | head moved: run again for the new head |
| 5 | closed: the pull request is closed or merged |
| 6 | could not read: no token, or reads failed on three polls in a row |
| 7 | no checks on the head by the second poll |

The session acts on the code without parsing prose: 0 goes on to the merge conditions, 2 is fixed
and pushed and the wait run again, 4 is run again, and 3, 5 and 6 stop the run. The guard that
bounds the fixing — the same check or lydite row failing again after a fix aimed at it stops the
run — stays with the session, which is the only party that knows what each fix was aimed at.

**It reads with a token, and only with a token.** The command reads with the first of `GH_TOKEN`
and `GITHUB_TOKEN` that holds a value, and never as the code-review App registration, even on a
machine that holds one — the same choice, for the same reason, as
`0028-a-backlog-item-is-trusted-by-who-wrote-it.md`. ADR 0019 reads as the App when it can
because a review's bookkeeping trusts markers by `ByViewer`, and only the App's installation makes
that field mean anything. Nothing here reads `ByViewer`; "who spoke last on a thread" is judged
against the token's own login, read once per wait. The registration would buy the wait nothing
and could cost it reads the review App holds no permission for. ADR 0019's other rule stands: the
command is built on the read client, and writes nothing to GitHub.

## Considered options

**The session polls for itself.** Sleep in the background, wake, read the checks, the lydite comment and the threads, decide, sleep again.
Rejected as the default: every poll is a model turn over the whole conversation, which is the
dominant cost of a phase that is mostly waiting, and the rule each turn applies is re-derived
from prose every time — a model reading a pending check as a pass once is a failing scan shipped
behind a closed handoff. It stays as the fallback for a session with no token.

**A shell script in the skill.** A `gh`/`jq` loop pasted into `SKILL.md` would cost no turns
either. Rejected: the rules — which conclusions pass, which status may stay pending, when a
thread is answered, when an empty head means no CI, how many failed reads end the wait — would
live as untested prose in a definition every consumer renders, each rule a quoting or `jq` error
away from reading pending as clean. In `agtk` they are Go behind a `Reader` and a `Clock`, tested
without a network or a sleep.

**A separate session for CI fixes.** Hand the open pull request to another session that waits and
fixes. Rejected: it starts without the plan, the tasks' reviews or the review loop's answers, and
would have to rebuild them to judge a failure. The fixes already go to fresh `task-implementer`s
with only the failure and its files in hand; what the coordinator holds is the judgement of which
failure belongs to which task and whether a fix is repeating, and that does not move to a session
that lacks it.

## Consequences

- **A cloud session falls back to polling for itself.** A claude.ai cloud session holds no
  `GH_TOKEN` or `GITHUB_TOKEN`; its GitHub access goes through other tools. The command exits 6
  at once, and step 7 polls every 5 minutes itself under the same clock, guard and verdicts,
  paying the turns this decision saves everywhere else.
- **Answered but unresolved threads wait clean.** A pull request whose every thread was answered
  by the token's account comes back clean with those threads counted. Resolving them, which
  approval needs, is left to a person, and the close-out report lists them.
- **The command never resolves, posts or approves.** It ends with a verdict and nothing on
  GitHub changes. Every fix, push and reply is the session's, through the same agents step 7
  already used.
- **A flaky failure ends a run.** A check that failed for a cause outside the branch reads the
  same on every new run of the wait, so the session reports it and stops rather than waiting out
  the cap on a failure it cannot fix.
- **The lydite comment never decides.** A comment from an earlier head cannot turn a red head
  green or a green head red; a repository whose lydite reports only through the comment, with no
  check or status on the head, is judged by its other checks alone.
