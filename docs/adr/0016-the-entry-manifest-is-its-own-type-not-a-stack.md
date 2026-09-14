# The entry manifest is its own type, not a Stack

A prior attempt gave the entry manifest a `local:` field, entry-manifest-only, enforced by a
runtime check on the shared `Stack` struct (`if ctx.Identifier != "" && st.Local != nil`) —
built that way because `.agentic-toolkit.yaml` and every `stacks/*.yaml` file were treated as
the same type. That shared type meant locally-scanned definitions could not reuse the identity
every other entry-manifest-declared item already gets for free — `StackName = ""`, which
already sorts last in `Plan.StackOrder` and already wins every order-dependent adapter merge —
because nothing at the type level distinguished "this field belongs only to the entry
manifest." `local:` had to invent its own `StackName` per category and specially append it to
`StackOrder`; getting that append right was a real bug in the attempt, caught before it ever
merged. The bug was not a one-off mistake — it was the cost of the shared-type decision, made
visible by the first feature that needed to tell the two apart.

The entry manifest is its own type now. Its category fields mean "scan `root/<category>/` by
convention" inherently — no `local:` key, no per-category path configuration, and no runtime
check to keep the semantics off a `Stack`, because a `Stack` simply has no such fields. `root:`
(default `"agentic"`), `context:`, `memory:`, and `platforms:` are native to the entry manifest
alone; a `Stack` reached through `extends:` cannot set them because they do not exist on that
type, not because a check refuses them. Composing shared content is `stacks:`, an entry-manifest
field, distinct from `extends:`, a `Stack` field — nothing composes *into* an entry manifest, so
there is no symmetry between the two worth preserving in one shared name.

The reasoning behind that first attempt was not wrong: an entry manifest's own content is a
fact about the consumer, not something a shared stack should be able to assert on its behalf.
That reasoning is preserved — it now lives in the type split itself, rather than in a
field-level restriction. Its mechanism is what changes: `root:` (default `"agentic"`),
`context:`, `memory:`, and `platforms:` are native to the entry manifest's own type alone. A
`Stack` reached through `extends:` cannot set them because they do not exist on that type, not
because a runtime check refuses them. Composing shared content is `stacks:`, an entry-manifest
field, distinct from `extends:`, a `Stack` field — nothing composes *into* an entry manifest, so
there is no symmetry between the two worth preserving in one shared name.
