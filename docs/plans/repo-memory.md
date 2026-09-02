# Plan — repo-resident long-term memory for agentic exploration

Status: Design. Nothing implemented.
Created: 2026-09-02.

A memory system that lives in the repo, so that an exploration an agent has already
done once is not paid for again. Three moving parts — an **explorer** that reads and
proposes, a **curator** that decides what survives, and a deterministic **`agtk memory`**
surface that does everything not requiring a model.

The existing `code-explorer` agent, `wrap-session` and `continuation-session` skills are
prior art we learn from, not a baseline we preserve. Where this design agrees with them
it is because they got it right; where it disagrees the disagreement is deliberate and
stated below.

---

## 1. The economics that decide every other question

Two costs, and they behave differently:

- The **index** is a tax. It is loaded on every session whether or not it is used.
- The **notes** are a payout. They are collected only on a hit.

Every design choice follows from wanting a thin tax and a rich payout. Two consequences,
both of which cut against the instinct to store more:

**Do not store what is cheap to re-derive.** Where a symbol lives, what calls what, which
build tool the repo uses — grep, the LSP and serena answer these in seconds. Storing them
inflates the tax and buys nothing. **Store the conclusions that cost twenty tool calls:**
invariants, the reason a surprising thing is the way it is, gotchas, and dead ends
someone already walked down.

This inverts the content policy of the current `code-explorer`, which saves exactly the
orientation-notes tier ("architecture, conventions, where-X-lives, build/test setup") and
explicitly forbids the expensive tier ("anything tied to an in-progress branch", "one-off
debugging context"). That rule was written to keep the file small, which was the right
instinct applied to the wrong axis: the fix for size is a thin index, not a ban on the
only content worth keeping.

**A wrong note is worse than no note.** It is confidently wrong and it short-circuits the
verification the agent would otherwise have done. That single fact justifies most of the
machinery below: anchors, curation, and the rule that every claim carries a pointer.

---

## 2. Store layout

Committed to the repo. Notes are then reviewable in PRs, they diff, and a note written on
a feature branch travels with that branch instead of leaking into `main`.

```
.agents/memory/
  INDEX.md          # generated; the only always-loaded file
  notes/
    lockfile-pins-shas-not-tags.md
  candidates/       # explorer's staging area; emptied by the curator
```

One fact per file. This is not tidiness — it is what keeps merge conflicts survivable in
a store that several branches write to concurrently. A monolithic map file conflicts on
every parallel session.

### Note format

```markdown
---
name: lockfile-pins-shas-not-tags
kind: invariant            # invariant | rationale | gotcha | dead-end
anchors:
  - path: internal/resolver/graph.go
    blob: a3f9c21
  - path: internal/lockfile/*.go
    blob: 77b1e04
confidence: verified       # verified | suspect
---

`agtk lock` resolves `extends:` graphs to commit SHAs, never tags — see
`internal/resolver/graph.go:88`. Anything that re-resolves at fetch time breaks
reproducibility for consumers. Tried and reverted in [[fetch-retag-attempt]].
```

Two rules about the body matter more than the schema:

- **Every claim carries a pointer.** `graph.go:88`, never "the resolver does X". This is
  what makes a note checkable in one `sed -n` instead of a re-exploration, and it is the
  main defence against a stale note being believed.
- **Link freely with `[[name]]`.** A link to a note that does not exist yet is not an
  error; it marks something worth writing.

### The index is generated, never authored

`agtk memory index` walks `notes/*.md` and emits `INDEX.md` from frontmatter: name,
one-line description, anchors, confidence. Nothing hand-writes it.

This buys two things. The index cannot drift from the notes, and a merge conflict in
`INDEX.md` is resolved by regenerating rather than by hand.

---

## 3. Anchoring: content hashes, not commits

Each anchor records the **blob hash** of the file it was derived from, not the commit the
note was written at.

Commit anchoring fails, and fails badly, under squash merges: the branch's commits become
unreachable and are eventually collected, so `git diff <commit>..HEAD` does not degrade —
it hard-fails with `unknown revision`. Rebases, force-pushes and shallow CI clones break
it the same way. Squash merge is the house default, so this is not a corner case.

Blob hashes are immune to all of it. Staleness detection becomes `git ls-files -s` over
the anchor set plus a set-difference to catch new files matching a glob anchor. One git
call, no history walk, and it works in a checkout with no history at all.

The cost: after a squash the previous blob may be gone, so re-verification sees "this file
changed, here is its current content" rather than a diff. That is acceptable — checking
the note's pointers against current state is the job anyway.

---

## 4. Reading

Two levels. `INDEX.md` is loaded; a note is read only when its anchors intersect the files
the task is about. Anchors therefore live in the index because that is where the routing
decision is made, and in the note because that is where provenance belongs — kept in sync
by generation, not by discipline.

Flat until it hurts. Past roughly 150 notes, shard the index per top-level directory and
load only the shards matching the task. Do not build sharding before the number justifies
it.

---

## 5. Writing: staged, then curated

The explorer appends to `candidates/` with no quality bar. Judging a finding is
deliberation in the hot path, and the explorer is mid-task with a conclusion that may not
survive the next hour.

The curator runs at session close and is the only writer to `notes/`. It promotes, merges
near-duplicates, rejects anything re-derivable or unverified, computes anchor hashes, and
regenerates the index.

Give the curator a **fresh context window** — it is fed the candidates plus the matching
slice of the index, not the session transcript. Asking for merge-and-reject judgment at
the exact moment context is most exhausted is how curation quietly stops happening.

---

## 6. Invalidation: lazy by default, sweep on demand

A stale note nobody reads costs nothing. Verifying it eagerly costs every time.

- **Lazy (the default path).** `agtk memory audit` is mechanical and model-free: compare
  recorded blob hashes to current, flip changed notes to `confidence: suspect`. Cheap
  enough to run from a hook. The explorer then refuses to trust a suspect note on read —
  it re-checks the pointers as part of the task it was already doing, and rewrites or
  drops the note.
- **Eager (deliberate).** `agtk memory audit --fix` walks every suspect note through the
  curator. For after a large refactor, or on a schedule.

Lazy is the default because it cannot be skipped by a crashed session, and because the
verification cost lands in the session that benefits from it.

---

## 7. Surface

**Deterministic — in the `agtk` binary, no model, ever:**

- `agtk memory index` — regenerate `INDEX.md` from note frontmatter.
- `agtk memory audit` — blob-hash comparison; flip changed notes to `suspect`.
- `agtk memory stats` — store size, suspect count, and **hit rate**.
- `agtk memory verify` — structural check for CI: index matches notes, anchors resolve.

`stats` is not optional. Without knowing how often a loaded note is actually used, there
is no way to tell whether the index tax is being repaid, and no basis for deciding whether
to prune. If hit rate stays low, the answer is prune harder — never store more.

**Model-driven — definitions, run by whatever agent the repo is configured for:**

- `explorer` agent — index-first lookup, anchor-scoped note reads, suspect-note
  re-verification, candidate writes.
- `memory-curator` agent — promotion, dedupe, merge, index regeneration.
- `/memory-seed` command — the cold-start pass (§9).

These stay out of the binary. `agtk`'s value is that `lock`/`fetch`/`render`/`sync` are
reproducible; putting a model call inside it trades that away for auth, cost, rate limits
and non-determinism. The rule: **LLM-invoking subcommands are a separate group, never on
the path of the deterministic ones, and never fired implicitly from a hook.**

The escape hatch for unattended runs is `--exec`: `agtk memory audit --fix --exec "claude -p"`.
`agtk` still knows nothing about models — it shells out to a configurable agent CLI, which
is also how `codex exec`, `opencode run` and `cursor-agent -p` slot in. Once
`agentic-driver` is usable, `--exec` is backed by it instead of by a raw string.

---

## 8. Session close

The curator has to be triggered by something, and Claude Code, Cursor and Copilot do not
agree on that lifecycle. This is the one piece of the design with no portable answer, and
it should be settled before the curator is built.

The lazy-by-default choice contains the damage: if the close hook never fires, candidates
accumulate and notes go unpromoted, but nothing becomes silently wrong. A design that
depended on the close hook for correctness would be a worse bet.

Worth noting that `continuation-session` already produces the high-value tier — its
handoff document is specified to capture "non-obvious constraints, approaches chosen, and
dead ends to avoid repeating; reasoning that lives only in this conversation" — and then
writes it to `$TMPDIR` as an explicitly throwaway artifact. The expensive knowledge is
already being extracted once per session and discarded. Feeding that same section into
`candidates/` is nearly free and independent of any hook.

---

## 9. Cold start

The store is empty for the first weeks, so it is pure tax before it is a win. Seed it:
one pass over the codebase writing the 15–20 highest-value invariants and gotchas. Beyond
priming the store, this is the first real test of whether the note format survives contact
with actual content.

---

## 10. Slices

**Slice A — store and deterministic surface.** Note schema, `agtk memory index`, `audit`,
`verify`, `stats`. No agents yet. Testable end to end with hand-written notes, which is
the point: the mechanical half should be correct before a model touches it.

**Slice B — explorer.** Index-first read, anchor routing, suspect handling, candidate
writes. Ship with seeded notes from Slice A so it has something to hit.

**Slice C — curator and session close.** Promotion, dedupe, fresh-context dispatch, and
whatever §8 resolves to. Redirect `continuation-session`'s decisions section into
`candidates/` here.

**Slice D — seeding and measurement.** `/memory-seed`, `--exec`, and enough `stats`
history to judge whether the tax is being repaid.

---

## 11. Open questions

- §8: what session close hooks into, per platform.
- Whether `explorer` replaces `code-explorer` outright or ships alongside it. Two stores
  that both claim to be repo memory is worse than one imperfect store, because neither
  becomes the one you trust — so probably replaces.
- Whether `stats` hit-rate tracking needs a written log, and where it lives so it does not
  itself become PR noise.

---

## 12. Unrelated bug found while surveying

`skills/wrap-session/SKILL.md` dispatches to the `wrap-session-reviewer` subagent, and
`definitions/agents/wrap-session-reviewer/` exists — but `stacks/default.yaml` lists only
`code-explorer` under `agents:`, so the reviewer never renders into a consumer. Confirmed
against a fresh consumer: `agentic-driver/main/.claude/agents/` contains `code-explorer`
alone. `wrap-session` is therefore broken in every consumer repo today. One-line fix,
worth doing independently of this plan.
