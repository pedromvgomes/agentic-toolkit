# Skill cost is attributed through span propagation across sessions

A skill causes spend in more than the turn that invoked it: its subagents run in their own
transcript files, and the work it shells out to (`agtk code-review`, `agtk memory curate`, a
`claude -p` in a `Bash` call) runs in model sessions with transcripts of their own. A dashboard
that answers "what did this skill cost" has to follow all of it, and has to say plainly which spend
it could not follow. The probe in `docs/spikes/skill-attribution-probe/` established what Claude
Code records and what crosses a process boundary; this decision is built on those observations and
on nothing else.

## Decision

**A skill run is a span, and a span's identity is `(session_id, span_id, skill)`.** `session_id` is
the session that loaded the skill. `span_id` is the `tool_use_id` of the `Skill` call, or the
`prompt_id` of the turn for a slash command. `skill` is the skill's name. The name alone is not an
identity: the same skill runs many times, and each run is costed separately.

**A span opens when the skill loads and has no recorded end.** The slash-command path fires
`UserPromptExpansion`; the `Skill` tool fires `PreToolUse`. Nothing fires when a skill finishes, and
the `PostToolUse` of the `Skill` tool fires when the body is *loaded*, so neither can close a span.
A span therefore ends at the next span start in its session, the next `UserPromptSubmit`, or `Stop`,
whichever comes first, and the collector computes that from the transcript rather than from a hook.

**Rows inside a session are attributed by the transcript, not by hooks or env.** Every assistant row
after a skill loads, in the main transcript and in `<session>/subagents/agent-<id>.jsonl` alike,
carries `attributionSkill`, and every assistant row carries `message.usage`. A span's own cost is the
sum of `usage` over the rows that carry its skill and fall inside its window, across the main file
and its subagent files. Subagent rows share the parent's `sessionId` and `promptId`, so nothing has
to be propagated to reach them.

**The span crosses into a nested session through a link record.** A nested session's rows carry no
`attributionSkill` and no reference to their parent, and the transcript does not record the
environment, so the link has to be written by something that knows both ends.

- When `agtk` spawns the child (`curator.Run`, `reviewrun`), it chooses the child's id: it passes
  `--session-id <uuid>` on every spawn and writes the link record itself, child `session_id` to the
  parent span's identity, before the child starts. It also exports the span in an environment
  variable. `Ambient()` children inherit it; an `Isolated` run carries it only if it is passed
  through `Invocation.Env`.
- When something else spawns a child that inherits the variable, a `SessionStart` hook in the
  child's project reads it and writes the same link record. `CLAUDE_ENV_FILE` is offered to
  `SessionStart` hooks only, so this hook is the only place a hook can act on the span, and it
  carries a per-session value, never a per-skill-run one.
- `--session-id` is mandatory for `agtk`-spawned children. A child that inherits
  `CLAUDE_CODE_SESSION_ID` adopts the parent's id and writes into the parent's transcript file when
  the cwd matches, so two sessions become one and neither can be attributed.

**Each row resolves to exactly one root run; nested runs are children.** A row belongs to the span
whose window and skill name contain it. That span's parent is, in order: the span whose `Skill`
call produced it in the same session, or, when its session has a link record, the linked span. The
**root** is found by following parents until a span has none; a row counts toward the root's total
and keeps its own span as the child it is shown under. A row in a linked session with no skill of
its own belongs directly to the linked span. Resolution is bounded: a chain deeper than a fixed
limit, or one that revisits a span, resolves to **unattributed** rather than looping. A root's total
is the sum over every row that resolves to it, so the dashboard's per-skill total is the sum of its
roots and never counts a nested row twice.

**Unattributed is an explicit bucket, never dropped and never spread.** Its rows keep their cost, the
reason they landed there, and the session they came from. A row is unattributed when:

- it carries no `attributionSkill` and its session has no link record, which covers all work done
  without a skill;
- it follows a skill's reply in the same turn and the transcript does not say the skill ended
  (see Known limits);
- it belongs to a nested session whose spawner neither is `agtk` nor exported the span, or whose
  link record was never written;
- it belongs to an `Isolated` run that was not given the span;
- its parent chain is cyclic or over the depth bound.

**The time-window join is the fallback for unlinked sessions only, and its result is marked.** A
session with no link record is assigned to a span when exactly one span window, in the same
container and `cwd`, contains the session's first row. Zero or several candidate windows leave it
unattributed. Every such assignment carries `basis: inferred`, so a root's total can be shown with
its inferred share separate from its linked share.

## Considered options

**Attribute only the invoking session's turns.** Rejected: it counts subagents, which share the
transcript's `sessionId`, but not the nested sessions the epic requires ("everything a skill causes
counts toward it"). `agtk code-review` and `agtk memory curate` are model sessions of their own, and
for the skills that call them they are most of the spend.

**A pure time-window join.** Rejected as the mechanism: it is ambiguous whenever skills overlap or
two children run concurrently, and cloud sessions share a container's clock and `cwd` across
children. It stays as the marked fallback because it needs nothing from the spawner.

**A per-skill-run environment variable set by a hook.** Rejected: `CLAUDE_ENV_FILE` is empty in
`UserPromptSubmit`, `UserPromptExpansion` and `PreToolUse` hooks, so env set at skill start cannot
reach a later `Bash` call. Env can carry only a value fixed at `SessionStart`.

**Hooks that open and close each span.** Rejected: no event marks a skill's end, and
`PostToolUse:Skill` fires at load. A span built from hooks would have a start and a guessed end;
the transcript already gives every row its skill.

**Identify a run by `prompt_id` or by skill name.** Rejected: one user turn can load several skills,
and one skill runs repeatedly, so either merges runs that must be costed apart.

**A hook as the only link writer.** Rejected for `agtk`-spawned children: `agtk` already chooses the
child's id and knows the span, so a hook adds a dependency on the child's project carrying it, and a
missed hook silently produces unattributed spend.

## Known limits

- `attributionSkill` has no end marker. Rows after a skill's last reply, in a turn where the model
  works without a skill, may keep the last skill's name; it was not observed past the skill's final
  reply, and this ADR does not claim they do or do not. Over-attribution of that tail is the
  expected failure.
- A child spawned by something other than `agtk` in a project without the `SessionStart` hook is
  unattributed, or inferred.
- The observations were made in a cloud session. Whether a local session inherits and adopts
  `CLAUDE_CODE_SESSION_ID`, whether `--session-id` is accepted for an id already in use in the same
  cwd, and whether an in-session nested `Skill` call carries the outer skill's `attributionSkill`,
  are unverified and are the first checks of the collector work.
- The Codex dialect of `agentic-driver` is out of scope: usage tracking covers Claude only.

## Consequences

- The collector reads transcripts and link records; it needs no per-event hook for in-session work,
  so a skill with no nested sessions is attributed with nothing installed.
- ADR 0002 holds: writing a link record is deterministic and calls no model.
- `agtk` gains a spawn-time duty: every model session it starts gets a chosen `--session-id`, a
  link record and the span variable, including on `Isolated` runs.
- The usage vocabulary in `CONTEXT.md` has to define **Span**, **Root run**, **Link record** and
  **Unattributed**, with the senses used here.
- The unattributed share is a number the dashboard reports on every skill view; a rising share is the
  signal that a spawner is not propagating, not noise to be filtered.
