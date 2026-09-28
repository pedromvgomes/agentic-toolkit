# A token may read a pull request; only the App posts to one

`agtk code-review` reaches GitHub for two different things. It reads a pull request — its
commits, its reviews, its comment threads — and it posts to one: a review, the file-level
threads that follow it, an approval. Both go through the App registration a machine holds, and a
machine holding none has nothing to do either with. A fresh container, a borrowed laptop, or an
agent session that starts from a clean home directory holds no registration, and usually does
hold a token in `GH_TOKEN` or `GITHUB_TOKEN` that can read the repository.

The two are not the same risk, because posting feeds a record and reading does not. ADR 0006
makes the pull request the only record between runs: approval reads what the last review found
out of that review's own marker, and a review run skips findings already posted by reading the
threads the pull request carries. That record is only worth reading if nobody but the App can
write to it, since the author of a change is the party a review does not trust. So every read
that decides something believes a marker only where the App's installation wrote it — the
`ByViewer` field on each review and thread:

- `reviewapprove.LastReview` selects the review of a head that `run --pr` treats as "already
  reviewed" and that `approve` requires to exist, and skips any review that is not `ByViewer`.
  `LastCompleteReview`, which a re-review reads on from, skips the same way — ADR 0018 requires
  every link of that chain to be this installation's own.
- `agtkThreads` keys a finding's thread for the approval gate only from a `ByViewer` root
  comment.
- `reviewpost.ReadThreads` gives a thread a fingerprint only when it is `ByViewer`, and
  `reviewrun`'s suppression and folding act only on a thread carrying one. A fingerprint marker
  in a comment somebody else wrote is a claim about identity from an author who does not hold
  it, and honouring it would let anyone silence a finding by typing its marker.

The memory note `.memory/notes/suppression-requires-a-complete-verdict.md` records the same rule
from the suppression side: a marker is believed from one account and read as prose from all
others.

## Decision

**A pull request can be read with a token; it can be posted to only as the App.** `explain --pr`
and the read-only paths of `run --pr` (`--dry-run`, `--no-post`) read as the App when this
machine is registered, and otherwise with the first of `GH_TOKEN` and `GITHUB_TOKEN` that holds
a value. `run --pr` without those flags, and `approve`, build only from the registration and
refuse before anything is read, whatever the environment holds.

**Only a machine with no registration at all falls back.** One holding an App id without its
private key is a registration somebody started, and it refuses with the "register again" message
rather than being read around. Reading under another identity would hide the breakage until the
first command that posts. `githubapp.Unregistered` is what tells the two apart, and it answers
false for an incomplete registration although that also matches `ErrNotInitialized`.

**The token client cannot post, by its type.** `githubapp.NewReadClient` returns a `ReadClient`
that has no method that writes and does not expose the `Client` inside it. `reviewapprove.GitHub`
and a run's `CreateReview`/`CreateFileComment` call sites fail to compile against it, so a token
reaching a post is a compile error rather than a runtime refusal somebody has to remember to
make. `TestATokenClientReadsAndNothingElse` fails if a write method is added to it.

**Nothing read with a token is by the viewer.** `ReadClient` clears `ByViewer` on every review
and thread it returns. `ByViewer` means the App's installation wrote it; under a token the viewer
is whoever owns the token, and left as GitHub answers it a token belonging to the change's author
would make the author's own comments read as the App's markers. Cleared, a token-backed read
contributes nothing to the bookkeeping above: no head reads as already reviewed, no review is
read on from, and no thread identifies a finding. Every such read errs toward doing more work,
never toward withholding any.

This amends ADR 0012's consequences for one case: a machine that holds no registration and
posts nothing. ADR 0012 places the App key on the machine as a once-per-machine registration and
concludes that a repository cannot be reviewed by someone who has not installed the App and
`agtk`, with bot review a property of the operator. Posting remains exactly that. Reading a pull
request, and running a panel against it without posting the result, no longer needs the
registration. Everything else ADR 0012 says about where the key lives and what a repository
holds is unchanged; the token is the operator's own ambient credential and is never written into
a repository.

It also states precisely what ADR 0006's "no model in a review run holds a GitHub credential"
protects. The App's key and the installation tokens minted from it stay in `internal/githubapp`,
which no model-invoking package can import. A token in the operator's environment is a different
matter, and one this decision does not change: `reviewrun` builds its drivers with
agentic-driver's ambient credentials, which inherit the parent environment, so a `GH_TOKEN` or
`GITHUB_TOKEN` the operator exported reaches every reviewer, judge and validator process whether
or not `agtk` reads it. The fallback reads the variable in `internal/cli` and puts nothing into
any environment — the guard banning `os.Setenv` and `os.Environ` walks `internal/cli` as well as
the credential surface. agentic-driver's isolated credentials would build the child environment
from a fixed list instead; the review pipeline does not use them.

Posting on a machine that holds no registration is outside this decision.

## Considered options

**Hand the App's private key to the container.** A setup script pipes it from a secret into
`code-review register --key-stdin`, and every command then works as on a registered laptop.
Rejected: the key mints installation tokens for every repository the App is installed on, with
`contents: write` (ADR 0009). ADR 0012 bounds its blast radius at one machine, and a key copied
into every short-lived container widens that to every container that ever held it — each of which
runs models reading a diff somebody else wrote, with a secret store that commonly delivers the
key through the same environment those models inherit. It spends the widest credential the
feature has to buy a read, which needs none of that reach.

**A hosted token broker.** A service holds the key and mints narrowly scoped installation tokens
on request. Rejected: it is standing infrastructure to run, secure and pay for, which is the cost
ADR 0012 rejects a self-hosted runner for. It also has to decide who may ask it for a token, and
the credential that answers that is the distribution problem again, one hop further away. For a
read, the token already in the environment is enough.

**Post as the token's owner when there is no registration.** The fallback would cover posting as
well as reading. Rejected for three failure modes, each of which follows from the `ByViewer`
reads above:

- A review posted that way can never be approved. `approve` requires a review of the head that
  is `ByViewer` under the App's installation, and one posted under another identity is invisible
  to it — the pull request carries a review the gate reports as absent.
- It is paid for twice. The next run as the App does not recognise the earlier review as its own,
  so it neither treats the head as already reviewed nor reads on from it, and spends a whole panel
  on a head that already carries a review.
- It inverts suppression. When the ambient token is the pull request author's own, `ByViewer` is
  true on the author's comments, and a fingerprint marker the author typed would read as the App's
  — withholding the finding it names on the author's say-so. That is exactly the silence the
  `ByViewer` gate exists to prevent, reached through the fallback rather than around it.

## Consequences

- A successful `explain --pr` on an unregistered machine is no promise that `run --pr` will post.
  The panel-code-review skill says so, and a posting run refuses before a panel is spent rather
  than after.
- `run --pr --no-post` under a token always reads the whole change and never finds the head
  already reviewed, whatever the App has posted. It costs a full panel where a registered run
  might cost a delta or nothing; it never costs a finding.
- A token that cannot see the repository is answered by GitHub as though nothing were there, so
  the error names the variable the token came from rather than implying the pull request is
  missing or that the fix is to register.
- A registration `Load` cannot use — an App id with no key, an id that does not parse, a key or
  directory another account can read — is refused on every path, reading included. The fallback
  answers only the machine that never registered.
