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

## Flagged ambiguities
**"Verify"** — was used both for the CI structural check and for a curator confirming a claim
is true. Resolution: the command is `agtk memory lint`; `verified` is reserved for
**Confidence**.

**"Stale" vs "suspect"** — two independent axes that an earlier draft collapsed into one
field. Resolution: **Stale** is mechanical and derived from blob hashes; **Confidence**
(`suspect`) is a curator's judgment. `agtk memory audit` reports the first and never writes
the second.

**"Root"** — `root:` in a **Stack** is the convention root for bare-name **Definition** lookups;
`memory.root` is the **Memory store** location. Unrelated; always qualify which.

## Example dialogue
> **Dev:** `graph.go` changed, so the note about SHA pinning is suspect now, right?
> **Domain expert:** It's *stale*, not suspect. Stale just means the anchored blob moved —
> `agtk memory audit` computed that from the working tree. Suspect is a curator saying "I kept
> this but I couldn't confirm it."
> **Dev:** So a verified note can be stale?
> **Domain expert:** Constantly. Verified is about who checked the claim; stale is about
> whether the file has moved since. The explorer re-checks the pointers on a stale note as
> part of the task it was already doing — that's the whole point of anchors being cheap.
