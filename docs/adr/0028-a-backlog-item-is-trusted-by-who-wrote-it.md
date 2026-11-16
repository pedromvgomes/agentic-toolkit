# A backlog item is trusted by who wrote it

A **Backlog item** is a GitHub issue, a sub-issue of a **Feature**, and its body carries the work:
the goal, the **Task**s and the verification command. The **Factory** — a separate repository,
`agentic-toolkit-factory` — takes a queued item to an open pull request with nobody starting it,
and the session its **Dispatcher** starts holds Write, Edit and Bash. The body therefore chooses
what an unattended session changes and which command it runs, which is the grant ADR 0014 found
in a **Handoff**, now reachable by anybody who can open or edit an issue.

ADR 0014 could answer "who wrote this" with a fact about the filesystem: a handoff is written
locally and never committed. An issue has no such fact. It lives on GitHub, and on a public
repository anybody can open one, comment on it and, through a pull request or a well-meant
suggestion, get text into it. The question is the same — was this document written by someone
trusted to direct the work — and the answer has to come from GitHub's own record of who did what.

So the factory asks `agtk backlog verify owner/repo#N` before every run, and acts on an item only
when it answers trusted. `agtk` reads that record and decides; it writes nothing to GitHub.

## Decision

**An item is trusted when all four of these hold, and refused otherwise.**

1. **The body is the work, and nothing else is.** Only the issue body is returned and run.
   Comments are never read as work, by `verify` or by the factory: a comment is the one place on
   an issue that anybody who can see it can write, and an instruction found there is prose from
   an author nobody checked.
2. **The author and every editor have write access.** The issue's author, and every account
   named in the body's edit history, holds `write`, `maintain` or `admin` on the repository. The
   permission is read at verify time, not at the time of the edit: an account whose access has
   been removed stops vouching for every body it touched.
3. **The queuing label came from a trusted actor.** The latest `agtk:queued` `labeled` event was
   applied either by the configured **Factory App** or by an account with write access. The App
   is named by `--trusted-app <slug>` and matched as the actor `<slug>[bot]`. Without
   `--trusted-app`, only write users count, and a label any App applied is refused. Only the
   latest event counts: an item queued, unqueued and queued again is judged by who queued it last.
4. **No edit is later than the queuing.** No body edit carries a timestamp later than that
   `labeled` event, and an edit at the same timestamp counts as later. GitHub's timestamps are
   to the second, so a tie cannot be ordered, and of the two readings it allows only "edited
   after" refuses the body somebody queued without seeing it.

The first condition decides which text is the work; the other three decide whether it is trusted.
Together they say: every hand that shaped this body is trusted, a trusted hand queued it, and
nobody changed it after that.

**CODEOWNERS plays no part.** Write access is what GitHub itself requires to edit the body of
somebody else's issue, so it is the boundary the record is already drawn on.

**The output is the body and its SHA-256.** A trusted answer carries the body exactly as verified
and the SHA-256 of its bytes. The factory hands both to the run, and the run compares the hash it
was given against the body it is about to act on and refuses a mismatch. The body that was
verified is the body that runs: an edit landing between `verify` and the start of the session
changes the hash, not the work.

**It reads with a token, and only with a token.** `verify` reads with the first of `GH_TOKEN` and
`GITHUB_TOKEN` that holds a value, and never as the code-review **App registration**, even on a
machine that holds one. That departs from ADR 0019's order, which reads as the App when the
machine is registered and falls back to a token only when it is not. The order there exists
because a review's bookkeeping trusts markers by `ByViewer`, and only the App's installation makes
that field mean anything; nothing here reads `ByViewer`, so the registration buys `verify`
nothing. It can cost a read: the review App holds the permissions a review needs, and may hold no
issue permissions on the repository at all, in which case reading as it fails where the token
succeeds. The Factory App is a different App, whose key has one holder (ADR 0020's reasoning,
applied to the factory), and `agtk` on an operator's machine is not that holder.

ADR 0019's other rule stands unchanged: a bare token only reads. `verify` is built on the read
client, and no command in this decision posts, labels or edits anything.

**Labels are applied by the skill, not by `agtk`.** `agtk` prints the canonical label set —
`agtk:stub`, `agtk:blocked`, `agtk:queued`, `agtk:running`, `agtk:in-review`, `agtk:failed`,
`agtk:needs-human` — and writes nothing. The `plan-feature` skill creates the labels on the
repository with `gh label create --force`, which creates a label or updates its colour and
description and never deletes one. Deleting is the operation to keep out of reach: deleting a
label strips it from every issue that carries it, so a sync that pruned unknown labels would
dequeue the backlog as a side effect. Keeping `agtk` read-only also keeps it outside the set of
actors condition 3 has to judge.

**Any read that fails refuses, as "could not determine".** An HTTP error, a GraphQL error, a
truncated page, a permission lookup that does not answer: each refuses the item, and is reported
as "could not determine", distinct from "not trusted". The two lead to different actions — one
means fix the reading, the other means a person has to look at the item — and an answer that
merged them would make an outage indistinguishable from an attack.

**A deleted account or a deleted revision refuses.** An edit whose editor is null — the account
has since been deleted, and GitHub reports it as the ghost user or no user at all — names nobody
whose access can be checked, so condition 2 cannot hold. A revision GitHub reports as deleted
hides what the body said and who said it. Both refuse; neither is skipped as though the edit had
not happened.

**This decides trust for backlog items, and only for them.** ADR 0014 governs local handoffs, and
continues to until a decision supersedes it. The two rules answer the same question for documents
that arrive by different routes, and neither reads the other's.

## Considered options

**Trust by CODEOWNERS.** Treat an item as trusted when its author owns the paths it touches.
Rejected: CODEOWNERS assigns reviewers to paths, and an issue has no paths — its body names files,
but which files it names is exactly what an untrusted author would choose. It would also make
trust depend on a file that the change being dispatched may itself edit, and leave every
repository without one with no rule at all.

**Keep the work in local handoff files.** ADR 0014's rule already holds for them, and needs no
GitHub read. Rejected for the factory's use: a file in somebody's worktree is not visible to a
runner in another repository, so an automatic loop has nothing to dispatch from. Handoffs keep
their rule for the sessions that do read them locally.

**Trust the label alone.** Applying a label takes repository access, so an `agtk:queued` item
could be taken as vouched for. Rejected on two counts. The access it takes is `triage`, below
`write`, so the label alone admits actors condition 2 refuses. And the label and the body are
written by different people at different times: an author without write access, or an edit made
after a trusted person queued the item, changes the work without touching the label, and the label goes on vouching for text its
applier never read. Conditions 2 and 4 exist to close exactly that gap.

**Fall back to REST when GraphQL is refused.** REST exposes the issue, its author, its timeline
and its labeled events, but not the body's edit history. A fallback would have to guess who
edited the body — assume nobody, or assume only the author — and either guess turns condition 2
and condition 4 into assumptions. Rejected: without the edit history the answer is "could not
determine".

## Consequences

- **A claude.ai cloud session cannot verify.** It holds no `GH_TOKEN` or `GITHUB_TOKEN`, and its
  proxy refuses GitHub's GraphQL endpoint, which is the only place the edit history is exposed.
  `verify` there answers "could not determine" — never "not trusted", since nothing about the item
  was learned. Verification runs where a token and GraphQL are both reachable, which is the
  factory's own runner.
- **Write access is a present fact.** Removing someone's access refuses every item they authored
  or edited, including ones already queued; granting it can make an item trusted that was refused
  before. Both follow from reading permissions at verify time, and both are what the boundary
  should mean.
- **Any edit after queuing requires re-queuing.** Fixing a typo in a queued item refuses it until
  a trusted actor removes and re-applies `agtk:queued`, so a person, or the Factory App, sees the
  body as it now stands. The same-second rule extends this to an edit made in the same second as
  the queuing.
- **Comments carry discussion, never instructions.** A correction to the work has to go into the
  body, by somebody with write access, followed by re-queuing; a reply in the thread changes
  nothing a run does.
- **The factory depends on the hash comparison.** `verify`'s answer is about one body. A runner
  that fetched the body again instead of using the one it was handed, or skipped the comparison,
  would run text that was never verified.
- **`--trusted-app` is the factory's to set.** Left unset, the Factory App's own transitions back
  to `agtk:queued` — sweeping a stalled run back to the queue, for one — read as untrusted, and
  the item waits for a person to re-queue it. That fails closed rather than open.
