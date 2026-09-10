---
description: Seed an empty memory store with the invariants and gotchas a newcomer to this codebase would get wrong.
argument_hint: "[area...]"
tools: [Bash, Read, Grep, Glob, Task, Agent]
---

Seed the repo's memory store: one sweep over the codebase that stages the durable facts an
agent would otherwise re-derive in every future session.

A store is pure cost until it holds something. The index is loaded on every delegation and
the notes pay out only when one is read, so a store with no notes is a tax with no payout.
This is the pass that gets it over that line, and it runs **once per repo** — after it, the
explorer stages findings as it works and the curator promotes them.

## 1. Locate the store and check it needs seeding

```bash
agtk memory stats --json
```

Read `root`, `project_root` and `notes` from the output. Never read `memory.root` from
`.agentic-toolkit.yaml`: a value reached through `extends:` is deliberately ignored, so the
manifest and `agtk` disagree.

- **`agtk` is not installed** — stop. There is nowhere to stage and no index to build.
- **The command exits non-zero** — stop, and report what it printed on stderr. `agtk memory
  stats` refuses rather than guesses when it cannot read the entry manifest, so one YAML typo
  produces this in a repo whose store is full. Seeding on top of that would duplicate every
  note it could not see.
- **`notes` is well above zero** — say so and ask before sweeping. This command exists for a
  cold start; run against a populated store it stages findings the store already holds, and
  the curator pays to reject them one by one.

If the user named areas in `$ARGUMENTS`, sweep only those and skip step 2's partitioning.

## 2. Partition the codebase into areas

Read the repo's own layout — its source directories, its build files, its `INDEX.md` if it
has one — and divide it into **4–8 areas grouped by concern**, not by directory count. An
area is one explorer's worth of reading: the packages that participate in one job, together.
Resolution and locking is an area. A 40-line version package is not.

Skip what the index already covers. A note that exists does not need staging again, and the
merge path is not free.

Say what the areas are before dispatching, so the user can correct a bad split cheaply.

## 3. Dispatch one explorer per area

Dispatch the `memory-explorer` agent once per area, **in parallel**. Give each one the area's
paths and this instruction, verbatim, because it replaces the bar the explorer normally
applies:

> Sweep this area of the codebase and stage what a future agent must not get wrong. This is a
> cold-start seeding pass, not a question, so your usual bar — *what cost real exploration* —
> does not apply: you are reading everything cold, so everything cost you exploration and the
> bar selects nothing.
>
> Use this bar instead: **would a competent engineer reading this code get this wrong?** Stage
> a finding only if the answer is yes, and say in the body what breaks when someone gets it
> wrong. If nothing in your area clears that, stage nothing and say so — an empty hand is a
> result.
>
> Every finding must be one of `invariant | rationale | gotcha | dead-end`; name which in the
> body. There is no target number. Do not stage where code lives, what calls what, a
> signature, or which files are in a package — grep answers those, and storing them adds to a
> cost paid on every future delegation.

Do not give the explorers a quota. A number is an instruction to keep going until it is met,
and the store is judged on hit rate, not size.

## 4. Report, and hand off

```bash
agtk memory candidates
```

Report what each area staged, and what it deliberately did not. Then stop.

**Do not curate.** `notes/` has exactly one writer and this command is not it. Tell the user
to run `/memory-curate --dry-run` to see what the sweep would produce, and `/memory-curate`
to promote it. The dry run is worth its cost here: a seeding backlog is the largest one the
curator ever sees, and the alternative is learning what it decided by reading what it already
did.

Never write, edit or stamp a note yourself, and never hand-edit `INDEX.md`.
