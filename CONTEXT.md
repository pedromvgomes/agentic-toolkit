# agentic-toolkit
A toolkit that distributes agent definitions (skills, agents, rules, hooks, MCP servers,
settings) from source repos into consumer repos, and a repo-resident memory store that
keeps what an agent learned by exploring a codebase.

## Language

### Distribution
**Definition**:
One unit of agent configuration — a skill, agent, rule, instruction, command, hook, MCP
server, or setting. Identified by a (category, name) pair.
_Avoid_: config, artifact, asset

**Category**:
The kind of a **Definition**. The set is closed: skill, agent, rule, instruction, command,
hook, mcp, setting.
_Avoid_: type, group

**Stack**:
A single YAML manifest listing **Definition** entries per **Category**, optionally layering
other stacks under it via `extends:`. Both a shareable stack and a consumer's own
`.agentic-toolkit.yaml` are stacks; there is no separate "preset" or "consumer config" concept.
_Avoid_: preset, profile, consumer config

**Entry manifest**:
The **Stack** that a given `agtk` invocation starts from — the consumer's own file, or the
one named by `--config`/`--stack`. Distinguished from stacks reached through `extends:`,
because some settings are honoured only here.
_Avoid_: root config, top-level stack

**Consumer**:
The repo that `agtk` renders into. Owns an **Entry manifest**, a lockfile, and its own
**Memory store**.
_Avoid_: client, target, downstream

**Platform**:
A target agentic-coding tool with its own on-disk layout that a **Stack**'s definitions can
render into — `claude`, `codex`, and (declared but not yet rendered) `cursor`, `copilot`,
`opencode`, `agents`. A **Stack** opts additional Platforms in explicitly (`platforms:`);
omitting the field renders `claude` only, unchanged from before Platforms existed. Distinct
from a **Reviewer**'s Provider (the coding-agent CLI a **Runner** invokes, see Code review
below): a Platform is about which files get written for a tool to read, never about
invoking a model.
_Avoid_: adapter, target, tool

**Adapter**:
The package (`internal/adapters/<platform>`) that **Render**s a resolved plan into one
**Platform**'s on-disk layout. Owns the mapping from each **Category** to that platform's
file conventions, and the policy where a canonical value has no native equivalent — a
converted construct, or a reported skip. The write/track machinery every Adapter shares
lives in `internal/adapters/fsops`.

**Render**:
Writing resolved **Definition**s into a consumer's layout for each of its opted-in
**Platform**s (`.claude/` + `CLAUDE.md` for `claude`; `.agents/` + `AGENTS.md` + `.codex/`
for `codex`), one **Adapter** run per Platform. The inverse direction of `fetch`.
_Avoid_: install, apply, generate

### Memory
**Note**:
One durable fact about the codebase, in one file, that cost real exploration to learn — an
invariant, a rationale, a gotcha, or a dead end. Never something grep or the LSP answers.
_Avoid_: memory, entry, fact file

**Kind**:
Which of the four a **Note** is: `invariant | rationale | gotcha | dead-end`.

**Anchor**:
A path (or glob) inside a **Note** recording the file the claim was derived from, together
with that file's git blob hash at the time it was stamped. The unit of both provenance and
staleness.
_Avoid_: reference, source, citation

**Stamp**:
Recording current blob hashes and glob expansions into a **Note**'s **Anchor**s. What
`agtk memory anchor` does, and the only sanctioned write to `notes/`.

**Stale**:
A **Note** whose **Anchor**ed content no longer hashes to the recorded blob. Derived on
demand from the working tree, never stored.
_Avoid_: outdated, dirty, invalid

**Confidence**:
The curator's judgment about whether a **Note**'s claim was checked: `verified | suspect`.
Independent of **Stale** — a note can be verified and stale, or fresh and suspect.

**Index**:
The generated `INDEX.md`, the routing table over the **Memory store**. One row per **Note**:
name, **Kind**, description, **Anchor** paths. Never hand-authored, and never loaded into a
session eagerly — an explorer reads it as its first step, so its cost is paid per delegation
rather than per session.
_Avoid_: map, catalog, manifest

**Candidate**:
A finding an explorer staged during a session, with no quality bar applied. Lives in
`candidates/` until a curator promotes it to a **Note** or rejects it. The explorer's only
write: an explorer never authors, stamps or deletes a **Note**.
_Avoid_: draft note, proposal

**Verdict**:
What an explorer concluded about an existing **Note** it re-checked because that note was
**Stale**: `still-true | now-false | unchecked`. Recorded on a **Candidate** so the curator
inherits the check instead of repeating it. Never written into the **Note** — that would be
writing **Confidence**, which is the curator's alone.
_Avoid_: status, result

**Hit**:
One read of a **Note** through `agtk memory show`. The numerator that says whether the
**Memory store**'s cost is being repaid.

**Memory store**:
The `.agents/memory/` directory holding the **Index**, `notes/` and `candidates/`. Committed,
so notes are reviewable in PRs and travel with the branch that wrote them.
_Avoid_: memory bank, knowledge base

**Explorer**:
The agent that answers understanding questions by reading the **Memory store** before
exploring, re-checks a **Stale** **Note** it relies on, and stages what it learned as a
**Candidate**. A **Definition**, so it runs inside the session that delegated to it.
_Avoid_: researcher, code-explorer

**Curator**:
The agent that turns **Candidate**s into **Note**s — promoting, merging near-duplicates, and
rejecting anything re-derivable or unverified — then stamps and regenerates the **Index**. The
only author of **Note**s, and the only thing that writes **Confidence**. Unlike the
**Explorer** it is not a **Definition**: it ships inside `agtk` and runs in its own process,
so it gets a context holding the candidates and the **Index**, never the session transcript.
_Avoid_: reviewer, promoter

**Curate**:
One run of the **Curator** over the staged **Candidate**s, or with `--stale` over **Stale**
**Note**s. The one `agtk` subcommand that invokes a model; every other one is deterministic
and safe on the path of a hook.
_Avoid_: promote, sweep

**Seed**:
One proactive sweep over a codebase whose **Memory store** holds no **Note**s yet, staging
**Candidate**s for a **Curate** run to rule on. Unlike an **Explorer**'s ordinary staging it
answers no question — so the cost bar that asks what an answer cost is meaningless to it, and
it asks instead what a competent reader would get wrong. A **Seed** never authors a **Note**.
_Avoid_: bootstrap, backfill, import

**Cold**:
A **Note** with no recorded **Hit**. The prune signal, once there have been at least as many
**Hit**s as there are **Note**s: a low hit rate says the store is not being repaid, and the
cold list says which **Note**s to drop. Below that threshold the list is non-empty by
arithmetic and says nothing about the notes in it.
_Avoid_: unused, dead, orphaned

### Code review
**Reviewer**:
One configured critic of a change — a name, a **Provider**, a model, and a prompt body. Declared
in the **Review manifest**. The unit that is spawned, and the unit a **Finding** is attributed to.
_Avoid_: agent, critic, panelist

**Runner**:
One configured model invocation — a **Provider**, a model and a prompt body. The shape a
**Reviewer**, the **Judge** and the **Validator** all take, because the three differ in what
they are asked and not in how they are launched. It says nothing about what a run may do:
every run in a review is read-only, and how that is enforced is a fact about the provider
rather than something a **Review manifest** can weaken.
_Avoid_: run, invocation, job

**Panel**:
A named set of **Reviewer**s, with how many instances of each to run and whether findings are
validated. One panel runs per review. Which one is a **Context**'s default, possibly raised by
an **Escalation**.

It may also name the **Judge** and the **Validator** that answer for it, instead of the ones the
**Review manifest** declares. A panel is how one context's reviewers are chosen, so it is where
the runs that reconcile them belong: the judge runs in every review, and without this a repo
reviewing locally with one **Provider** and its pull requests with another could say so for its
reviewers and not for its judge. Both are overrides — a panel naming neither uses the
manifest's, so declaring them on one panel is never the price of declaring them on all.
_Avoid_: profile, preset, tier

**Context**:
What a review runs against — the local working tree, or an open PR. It names the default
**Panel**, and it decides what a run is obliged to do rather than what it may: a context that
posts always validates. One manifest therefore describes both the pre-push review and the PR
review.
_Avoid_: environment, mode, target

**Escalation**:
A rule that raises the **Panel** above the **Context**'s default when a change meets its
condition. Rules only ever raise, so a mistaken rule costs money and never yields a shallower
review than the default; every rule is evaluated and the highest target wins, so their order
carries no meaning.

"Highest" is what a **Panel** spends — its **Reviewer** count times its **Quorum**. Depth needs
a total order over panels and panels carry only names, so the order is the thing "deeper"
already meant. A declared rank would be a second thing to keep true, and a manifest whose
`deep` panel was cheaper than its `standard` one would then be ordered by an adjective rather
than by what it does. Equal cost is not a raise.
_Avoid_: matcher, trigger, override

**Signal**:
A property of a change that `agtk` detects itself — a touched concern like `auth`,
`concurrency` or `fix-revert`. The vocabulary is closed and ships with the binary, because
detecting one is language knowledge that has to be tested somewhere other than a consumer's
YAML. A repo names paths instead.
_Avoid_: heuristic, marker, flag

**Exclusion**:
A changed file no **Reviewer** is shown, and which counts toward nothing a rule measures. Most
are mechanical — the file changed, but nobody wrote the change: a lockfile, a vendored tree, a
generated file, a binary, a pure rename, a symlink. That vocabulary is closed and ships with
the binary, for the reason **Signal**'s is.

A repo adds its own as globs, for the one thing detection cannot reach: source a person wrote
that is not worth a review's budget. It only ever removes files, which is the opposite
direction from an **Escalation**, so it is declared in the **Review manifest** rather than
given on a command line — committed where anyone can read it, and read from the base ref so a
branch cannot exclude itself. A repo's own reason is reported ahead of a mechanical one,
because only it points at a line somebody can edit.
_Avoid_: ignore, skip, filter, exemption

**Finding**:
One issue a **Reviewer** reports: a file, a line range, a severity, and a body. The unit
**Judge**ment is applied to and the unit that becomes an inline comment.
_Avoid_: issue, comment, result

**Judge**:
The single run that reads every surviving **Finding**, merges near-duplicates, sets final
severity and decides which reach the PR. It reconciles a set; judging one claim on its own
evidence is the **Validator**'s job, and the two are separate because they are different
questions. It decides; it does not transmit — the App credential never
enters a model's process, so `agtk` alone calls GitHub. The same separation of authority from
action that ADR 0003 makes for the **Curator**.
_Avoid_: reducer, arbiter, referee

**Quorum**:
How many independent instances of each **Reviewer** a **Panel** runs. Agreement between them is
the confidence signal: a **Finding** two instances reach independently is corroborated, and
corroboration is what spares it from demotion.
_Avoid_: duplicates, redundancy, disputed

**Validator**:
A run handed one candidate **Finding** and its evidence and asked whether it holds. Optional per
**Panel**, and on where a **Review** is posted: a false finding there is published and blocks
**Approval**, rather than merely cluttering a terminal. Independent of the **Judge** by
construction — it sees one claim, not the set — which is the whole of what it adds.
_Avoid_: verifier, checker, second pass

**Review**:
The one artifact a review run posts: a single GitHub review with event `COMMENT`, carrying a
summary body and one inline comment per surviving **Finding**. A review run posts nothing else
— **Approval** is a separate act, reachable only from its own subcommand.
_Avoid_: report, verdict, comment

**Severity**:
How much a **Finding** matters: `RED | AMBER | GREEN`. RED and AMBER are both defects and
differ in the strength of the claim rather than in what they oblige: each must be fixed, or
marked a **False positive**, before **Approval**. GREEN is a remark, and obliges only that its
**Comment thread** be resolved.

AMBER is the default **Approval floor**, so "at or above the floor" reads as "a defect rather
than a remark". A repo that wants only RED to oblige a fix sets the floor to RED.
_Avoid_: priority, level

**Approval**:
A GitHub review with event `APPROVE`, posted by the App so it counts toward a repo's required
approvals — which a solo author cannot satisfy alone, since nobody may approve their own PR.

It counts only because the App can push. GitHub weighs a review by whether its author has write
access, and drops one that does not out of the set it decides from, so the App's write grant on
repository contents is what makes an approval an approval rather than a decoration. See ADR
0009.

Granted only when a **Review** exists for the PR's current head commit and reached a verdict,
every **Finding** it reports at or above the **Severity** floor is marked a **False positive**,
and no **Comment thread** on the PR is unresolved. Nothing overrides any of it. A defect is
cleared by changing the code, and a wrong **Finding** by saying so on the PR, and those are the
only two ways: there is no flag that approves anyway, because one would make the whole of this
a checklist rather than a control.

Never reachable from a review run. No model decides it, no tool grant contains it, and the
**Judge** cannot reach it: a run that could approve the code it just reviewed is the hazard
GitHub blocks `GITHUB_TOKEN` approvals to prevent. The person types the command.
_Avoid_: sign-off, gate, merge

**False positive**:
A **Finding** somebody with write access has said is not a defect, marked by replying on its
**Comment thread**. It clears that finding for **Approval** and nothing else: the thread
stays, the comment stays, and a later **Review** reports the finding again while the code still
quotes the same evidence.

A reply rather than resolution, because the two are different claims. Resolving says the
conversation is finished; it does not say the defect was never there, and a **Severity** at or
above the floor is a defect until somebody writes down that it is not.

Write access rather than anyone who can comment, because the author of a change is the party a
review does not trust. A **Finding** its own author could dismiss is one an injected
instruction can dismiss too, and that is the conversion ADR 0007 exists to prevent, reached at
the last step instead of the first.
_Avoid_: suppression, waiver, dismissal, ignore

**Review root**:
The copy of the code under review that `agtk` writes outside the project directory, one file
at a time from `git ls-tree`. No **Reviewer** ever runs with it as its working directory —
that is the **Review workdir** — and the instruction files a coding-agent CLI discovers by
walking upward (`AGENTS.md`, `AGENTS.override.md`, `TEAM_GUIDE.md`, `.agents.md`,
`CLAUDE.md`, `.codex/`, `.claude/`) are never written into it, so there is no window in which
they exist to be neutralised.

Written rather than checked out, because both ways of checking out hand the reviewed branch
something. A `git worktree` leaves a `.git` behind, which gives a codex reviewer the whole
repository through a directory the sandbox has no reason to refuse. `git archive` honours the
reviewed head's own `.gitattributes`, so `export-ignore` lets a branch hide files from the
review and `export-subst` lets it rewrite them. Codex has no equivalent of
`--setting-sources ""`, so discovery is closed by where the child runs and what that directory
holds, not by a flag.

A symlink or a gitlink in the tree is refused and named rather than written: either one makes
"the reviewed code is a copy outside the project directory" untrue, since following it leads
back out.
_Avoid_: checkout, workspace, head, worktree

**Review workdir**:
The empty directory a **Runner**'s child process actually runs in — a sibling of the **Review
root**, holding nothing and belonging to no repository. It is what makes "no reviewer runs
with the reviewed code as its working directory" a fact about the filesystem rather than an
instruction: everything under the review root is material a run opens by absolute path, and
nothing above the workdir is a project to walk up into.
_Avoid_: scratch dir, sandbox, cwd, temp

**Fingerprint**:
What identifies a **Finding** across runs: its path, its category and the code it quotes,
hashed. Not its line, which moves on every push, and not its prose, which differs between two
runs describing one bug — so identity means "this code, this kind of problem". Carried in a
posted comment so a later run reads it rather than re-deriving it.
_Avoid_: id, key, hash

**App registration**:
The GitHub App id and private key one machine holds, in agtk's own config directory. What a
**Review** is posted as. Registered once per machine rather than once per repository, and
never written into a repository — a fork, a clone or a leaked secret scan has nothing to
find. The key is readable by its owner alone, and one any other account can read is refused
rather than used: the blast radius of an App key is one machine, and a key a second account
can read makes that untrue.

The short-lived installation token minted from it is held in memory for one run and written
nowhere. It reaches every repository the App is installed on, so it never enters a model's
process — a **Reviewer** inherits the operator's environment, which is why the token is
passed as an argument rather than placed in one.
_Avoid_: secret, credential file, PAT

**Fingerprint marker**:
The HTML comment a posted inline comment carries its **Fingerprint** in, invisible in
rendered markdown, so a later run reads identity off the PR rather than re-deriving it. It
names the scheme's version, because a change to what is hashed makes every existing marker
mismatch — and without a version that reads as "every finding is new" rather than as "the
scheme moved".

Two words, because the bare noun belongs to **Signal**, which lists it under `_Avoid_`. A
comment carrying a fingerprint and a detected property of a change are unrelated things, and
a glossary that gave them one word would ban it for one of them and canonise it for the
other.
_Avoid_: tag, sentinel, watermark, marker (bare)

**Comment thread**:
One conversation on a pull request's diff, rooted at the comment that opened it. State is read
back off the pull request on every run and never cached: the pull request is what a person
actually looked at, and it survives a fresh clone. Its state is what a re-review turns on — an
**open** or a **resolved** thread carrying a **Finding**'s **Fingerprint** withholds that
finding, and an **outdated** one does not, because GitHub collapses an outdated thread and the
finding is invisible where the code now lives.

A thread hangs off a line, or off a whole file where a **Finding** names no line the change
adds. The second is the only way a finding stated in the **Review** body becomes something a
person can answer, and a finding naming a path the change does not touch can be neither — it is
stated in the body alone, and blocks nothing, because `agtk` can offer no way to answer it.

Resolution ends a conversation; it does not clear a defect. **Approval** requires every thread
resolved, and separately requires each **Finding** at or above the floor to be marked a **False
positive** — so resolving is necessary for approval and never sufficient.

Only a thread the App itself opened carries an identity. Anyone who can comment on a pull
request can type the characters that open a **Fingerprint marker**, and one naming a finding's
fingerprint would make a review silent about code somebody chose without touching the code.

Read over GraphQL. REST reports neither resolution nor staleness — it answers `position: null`
for a comment whose code has moved and says nothing at all about resolution — so two of the
four states have no REST answer, and they oblige opposite things.
_Avoid_: conversation, discussion, comment chain, review thread

**Suppression**:
A **Finding** withheld from a **Review** because a **Comment thread** already carries its
**Fingerprint**. Deterministic and `agtk`'s: it happens before the **Judge**, so a withheld
finding is never issued an id and the judge — which answers with ids `agtk` issued — cannot
restore one. The judge narrows further and never widens.

One-directional in the other sense too: a thread list that could not be read withholds nothing
and says so. "Nothing was withheld" is what a clean pull request produces and what a failed
read produces, and only one of them means the pull request is clean.

A `security:prompt-injection` finding is never withheld. ADR 0008 carves it out of the judge's
reach because a dropped one converts an injected instruction into a clean review, and a
suppressor that removed it first would open that hole from the other side.
_Avoid_: dedupe, skip, filter, squelch

**Review marker**:
The HTML comment a posted **Review**'s body carries, naming the commit reviewed, whether the
run reached a verdict, every surviving **Finding** by **Fingerprint** and **Severity**, and
which of them `agtk` could give no **Comment thread** to. It is how **Approval** learns what the
last review found, since nothing is persisted and the pull request is the only record.

The last of those is what keeps the gate from demanding an answer nobody can write: a
**Finding** with no thread blocks nothing, and a `security:prompt-injection` finding with no
thread blocks everything.

In the body rather than in the comments, because a **Review** is one statement about one commit
and some of its findings never become comments at all. A per-comment record could not carry a
finding with no line, and could not say that a run reached no verdict — which is the one thing
approval must never read as a clean review.
_Avoid_: summary, header, footer, marker (bare)

**Review manifest**:
`.agents/code-review/manifest.yaml`: the single declaration of **Reviewer**s, **Panel**s and the
prompt bodies they use. Read by `agtk code-review` and by nothing else. A roster a skill also
carried would be a second one, and the two would disagree the first time either changed.
_Avoid_: panels.json, roster file, review config

### Feature flow
**Handoff**:
A document that lets a session with no memory of the one that wrote it continue the work.
Carries a goal, the current state and the next steps; optionally a plan's **Task** list, its
**Slice**s and a **Predecessor**. The optional half is what keeps one writer sufficient: a
handoff written by hand mid-work carries none of it and is still a handoff, read as a
one-**Task** list.

Lives in `handoff/` at the worktree root, excluded through the repo's `info/exclude` rather
than its `.gitignore` — the folder is a fact about how somebody works, not about the project,
and a **Consumer** that never adopted the flow should not carry a line for it.
_Avoid_: continuation doc, session doc, context dump

**Predecessor**:
The pull request a **Handoff**'s work may not start before, merged into the base branch. A
handoff written while a PR is open has one; a handoff written with no PR open has none, and
its session resumes on the same branch immediately.

It decides *waiting*, not *where*: the worktree is a constant, so a handoff with a predecessor
resumes by branching off the updated base in the same directory. Reading it is automatic and
moving the branch is not — the first spends nothing, and the second changes where somebody is
standing.

Not "gate", which **Approval** already lists as a word to avoid. The two are different enough
that one word for both would be a real loss: a predecessor is a fact about ordering that any
session can read from GitHub, and approval is a control that no model may reach.
_Avoid_: gate, dependency, blocker, prerequisite

**Slice**:
One pull request's worth of a plan. A plan spanning several slices names them in landing
order, and each slice becomes its own **Handoff**, whose **Predecessor** is the slice before
it. A plan of a single slice has no predecessor and never writes a second handoff.
_Avoid_: phase, stage, milestone, chunk

**Task**:
The unit one **Implementer** is given: a description, the files it may touch, and a
**Complexity**. Tasks land in order and one at a time, so a task is also the unit that gets
committed, reviewed, or sent back.
_Avoid_: step, item, ticket

**Complexity**:
Which model a **Task**'s **Implementer** runs on: `routine | intricate`, for sonnet and opus.
Two values rather than three, because a third would need a rule for what it selects and there
is no third model in the flow to give it.
_Avoid_: difficulty, size, weight, effort

**Coordinator**:
The main session that reads a **Handoff**, spawns one **Implementer** per **Task**, reviews
what comes back and decides whether it lands. Never a subagent — not because nesting is
refused, since it is allowed three layers deep, but because the coordinator's model is the one
thing in the flow that outlives a single call, and a delegated coordinator would hold the
implementers' transcripts in the context the flow exists to keep clear.
_Avoid_: orchestrator, driver, parent

**Implementer**:
The subagent handed exactly one **Task** and the files it may touch. It returns a short
structured report and nothing else: its transcript never reaches the **Coordinator**, which
reviews the diff instead. Denied the `Agent` tool, so "one implementer at a time" is a fact
about its tool set rather than a sentence it is asked to honour.
_Avoid_: worker, executor, builder

## Flagged ambiguities
**"Marker"** — the bare noun is a **Signal** synonym to avoid; the HTML comment that carries a
**Fingerprint** is a **Fingerprint marker**, always both words.

**"Exclusion" vs "Suppression"** — both withhold, and they withhold different things at
different ends of a run. An **Exclusion** is about a *file*, decided before any reviewer runs:
the file is never shown, so no **Finding** about it exists. A **Suppression** is about a
*finding* that was made, withheld from a **Review** because a **Comment thread** already
carries its **Fingerprint**. An excluded file produces nothing to suppress, and a suppressed
finding came from a file that was reviewed.

**"False positive" vs "Suppression"** — both withhold something, and they are opposite acts.
**Suppression** is `agtk`'s and mechanical: a **Finding** is not posted again because a thread
already carries it. A **False positive** is a person's and is about the claim itself: the
finding is posted, stays posted, and is declared not to be a defect. Suppression never affects
**Approval**; a false positive is the only thing that clears a defect without a code change.

**"Candidate"** — a staged memory finding awaiting a **Curator**, and also a **Finding** that
has not yet passed a **Validator**. The memory sense owns the bare noun; in review, say
"candidate finding" and never "candidate" alone.

**"Panel"** — reads as a per-stack group of reviewers *within* one run, so that a polyglot
change would have several. A **Panel** is the entire roster for a run, and exactly one runs:
the one a **Context** defaults to, possibly raised by an **Escalation**. The per-stack sense
has no name because per-stack partitioning is not built.

**"Review"** — the activity and the artifact. **Review** is the artifact posted to the PR; say
"review run" for the activity, and **Approval** is never part of either. A **Comment thread**
is never a "review thread" for the same reason, even though that is GitHub's own field name —
the wire type in `internal/githubapp` keeps GitHub's spelling because it is what GitHub
answers with, and nothing else does.

**"Judge" vs "Curator"** — both are single model runs holding final authority over what
survives, and neither performs the act its judgment authorises. Deliberately parallel; they
share no code and no store.

**"Verify"** — was used both for the CI structural check and for a curator confirming a claim
is true. Resolution: the command is `agtk memory lint`; `verified` is reserved for
**Confidence**.

**"Stale" vs "suspect"** — two independent axes that an earlier draft collapsed into one
field. Resolution: **Stale** is mechanical and derived from blob hashes; **Confidence**
(`suspect`) is a curator's judgment. `agtk memory audit` reports the first and never writes
the second.

**"Root"** — `root:` in a **Stack** is the convention root for bare-name **Definition** lookups;
`memory.root` is the **Memory store** location. Unrelated; always qualify which.

**"Agent"** — a **Definition** **Category**, and also the thing that runs one. `memory.agent`
is neither: it names which coding-agent CLI `agtk` drives when it **Curate**s. Say "provider"
for that, and qualify the other two.

**"Absence" vs "quantified"** — the rule for when an **Anchor** takes a glob was written as
"when the claim is about the absence of something", which nobody applies to a sentence like
"every command except `lock` uses the frozen provider". Resolution: the trigger is
**quantification** over a file set — *every*, *only*, *no* — because a member that does not
exist yet is what falsifies such a claim. See `docs/adr/0005-glob-anchors-mark-quantified-claims.md`.

**"Hit rate"** — reads as a property of the **Memory store**, but the **Hit** log is local to
one checkout and never committed, so a fresh clone reports zero. Resolution: it is a fact about
one working copy's usage, and any claim that the store is or is not repaying its cost has to
say whose. It carries a second scope that reads the same way: *n* **Hit**s can warm at most *n*
**Note**s, so a rate below one read per note is bounded by how much reading has happened rather
than by how good the notes are, and **Cold** is empty of information over the same range. Both
scopes have to hold before a rate is evidence for pruning.

**"Task"** — a unit of work inside a **Handoff**, and also the name of the Claude Code tool that
spawns a subagent. The glossary sense owns the bare noun; call the mechanism "the Agent tool",
never "a Task", so that "one task at a time" stays a statement about work and not about calls.

**"Review" in the feature flow** — one change is reviewed twice, and neither pass is a new
sense of the word. The local loop before a pull request exists and the pass posted to it
afterwards are both review runs; **Review** is still only the artifact posted, and only the
second produces one. They differ in **Panel** and therefore in which model family reads the
change, which is the point of running both.

## Example dialogue
> **Dev:** `graph.go` changed, so the note about SHA pinning is suspect now, right?
> **Domain expert:** It's *stale*, not suspect. Stale just means the anchored blob moved —
> `agtk memory audit` computed that from the working tree. Suspect is a curator saying "I kept
> this but I couldn't confirm it."
> **Dev:** So a verified note can be stale?
> **Domain expert:** Constantly. Verified is about who checked the claim; stale is about
> whether the file has moved since. The explorer re-checks the pointers on a stale note as
> part of the task it was already doing — that's the whole point of anchors being cheap.
