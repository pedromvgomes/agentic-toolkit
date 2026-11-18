# A Workflow Definition renders nothing on any platform

`workflows:` is a **Definition** category like the other eight: a **Stack** lists Workflow
Definitions under it, `extends:` layers them, the resolver dedupes them and the lockfile pins
their sources. ADR 0015 holds that every category lands somewhere on each **Platform** a stack
opts into. A **Workflow Definition** is the one category that does not, and this records why.

Every other category is read by the coding agent the render targets: Claude Code reads
`.claude/` and `CLAUDE.md`, Codex reads `.agents/`, `.codex/` and `AGENTS.md`. Nothing on either
platform reads a Workflow Definition. What reads it is whatever starts a **Workflow Execution** —
the **Factory**, running outside the repository that declares the definition — and it needs the
document in hatua's format, byte for byte, as resolved for that repository's stacks.

## Decision

**A Workflow Definition renders nothing.** Neither **Adapter** writes a file for one, claims a
managed key or region for one, or records a manifest entry for one. A resolved set holding
Workflow Definitions renders exactly the tree it renders without them, its dry run prints the same
lines, and `agtk status` reports no drift for them.

**Each adapter says so.** The Claude and Codex adapters both name `CategoryWorkflow` in every
switch that maps a category to its output, as a case that writes nothing, rather than letting it
fall through an absent default. A reader of either adapter sees the decision where the mapping is
made, and the next category added has a precedent to be explicit against.

**The source is read through `agtk workflow show <id> --json`.** The command resolves the entry
manifest and its stacks the way `render` does — layering, dedupe, lockfile pins — and prints the
winning definition's source. A caller gets the same answer `render` would compute and never
re-implements stack resolution to find it.

## Considered options

**Rendering a copy into each platform's directory.** Rejected. Neither Claude Code nor Codex
reads a Workflow Definition, so the copy has no reader on the platform it is written for. It
would be a second, rendered source of the same document: one the Factory could read instead of
asking `agtk`, gitignored under the default policy so absent on a fresh clone, and stale the moment
a stack it came from moves. It would also add whole-owned files to every render's manifest,
collisions and stale removal, for output nothing consumes.

**A platform-neutral output directory, such as `.agtk/workflows/`.** Rejected for the same
reason with one addition: it is a render target that belongs to no Platform, so `platforms:`
could not say whether a stack writes it, and ADR 0015's dispatch has no adapter to send it to.

**Leaving the category to fall through the adapters' switches.** This is what happens when an
adapter names nothing for a category, and it already produces the right output. Rejected as the
way of getting it, because the outcome is then an accident of the switch having no default: a
reader cannot tell a category deliberately rendered as nothing from one nobody remembered, and
ADR 0015 reads as violated. A default that errors on an unknown category would turn the same
silence into a failed render instead.

**Having the Factory resolve stacks itself.** Rejected. Layering, dedupe and lockfile pinning
are `agtk`'s, and a second implementation drifts from the first the first time either changes:
the Factory would run a different Workflow Definition from the one `agtk` reports for the same
repository.

## Consequences

- ADR 0015's "every category lands somewhere" holds for every category but this one. A future
  category with no reader on any Platform follows this decision rather than ADR 0015, and its
  adapters say so the same way.
- A Workflow Definition's source never appears in `.claude/`, `.agents/`, `.codex/`, `CLAUDE.md`
  or `AGENTS.md`, so the rendered-output policy has nothing to say about it: it is committed where
  it is authored, under the entry manifest's `<root>/workflows/` or a stack's source, and nowhere else.
- Adding or removing a Workflow Definition from a stack never changes a rendered file, so it
  never produces a rendered diff, a collision refusal or a stale removal.
- `agtk workflow show` is the only supported way to read a resolved Workflow Definition.
  Reading a stack's `workflows:` list and fetching the file directly skips the dedupe and the
  lockfile pin, and can return a definition `agtk` would not.
