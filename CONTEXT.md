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

**Render**:
Writing resolved **Definition**s into a consumer's platform-specific layout (`.claude/`,
`CLAUDE.md`, a manifest). The inverse direction of `fetch`.
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
How much a **Finding** matters: `RED | AMBER | GREEN`. RED means must fix before merge, and is
the default **Approval floor**.
_Avoid_: priority, level

**Approval**:
A GitHub review with event `APPROVE`, posted by the App so it counts toward a repo's required
approvals — which a solo author cannot satisfy alone, since nobody may approve their own PR.
Granted only when a **Review** exists for the PR's current head commit and no **Finding** at or
above the **Severity** floor survived, unless forced.

Never reachable from a review run. No model decides it, no tool grant contains it, and the
**Judge** cannot reach it: a run that could approve the code it just reviewed is the hazard
GitHub blocks `GITHUB_TOKEN` approvals to prevent. The person types the command.
_Avoid_: sign-off, gate, merge

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

**Marker**:
The HTML comment a posted inline comment carries its **Fingerprint** in, invisible in
rendered markdown, so a later run reads identity off the PR rather than re-deriving it. It
names the scheme's version, because a change to what is hashed makes every existing marker
mismatch — and without a version that reads as "every finding is new" rather than as "the
scheme moved".
_Avoid_: tag, sentinel, watermark

**Review manifest**:
`.agents/code-review/manifest.yaml`: the single declaration of **Reviewer**s, **Panel**s and the
prompt bodies they use. Read by both engines — the in-session skill and `agtk code-review` — so
there is one roster and not two.
_Avoid_: panels.json, roster file, review config

## Flagged ambiguities
**"Candidate"** — a staged memory finding awaiting a **Curator**, and also a **Finding** that
has not yet passed a **Validator**. The memory sense owns the bare noun; in review, say
"candidate finding" and never "candidate" alone.

**"Panel"** — `deep-code-review` used it for a per-stack group of reviewers *within* one run,
so a polyglot change had several. A **Panel** here is the entire roster for a run — one runs,
named by a **Context**'s default and possibly raised by an **Escalation**. The per-stack sense
has no name because per-stack partitioning is not built.

**"Review"** — the activity and the artifact. **Review** is the artifact posted to the PR; say
"review run" for the activity, and **Approval** is never part of either.

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

## Example dialogue
> **Dev:** `graph.go` changed, so the note about SHA pinning is suspect now, right?
> **Domain expert:** It's *stale*, not suspect. Stale just means the anchored blob moved —
> `agtk memory audit` computed that from the working tree. Suspect is a curator saying "I kept
> this but I couldn't confirm it."
> **Dev:** So a verified note can be stale?
> **Domain expert:** Constantly. Verified is about who checked the claim; stale is about
> whether the file has moved since. The explorer re-checks the pointers on a stale note as
> part of the task it was already doing — that's the whole point of anchors being cheap.
