# `local:` is entry-manifest-only, and refuses rather than ignores elsewhere

`local:` lets a stack declare per-category directories (and one file, for `context:`) that
`agtk` scans instead of requiring every definition to be listed by name. Its paths would
resolve safely on any stack — relative to the stack file that declares them, the same rule
`./path` entries already follow — so a shared stack scanning its own directory poses no risk
to a consumer's tree. Despite that, `local:` is restricted to the **entry manifest** only,
and a shared stack reached through `extends:` that sets it fails the render outright, rather
than being ignored with a diagnostic the way `memory:` is.

The restriction is deliberate, not technical: which directories make up *this* repo's own
definitions is treated as a fact about the consumer, not something a stack it merely imports
should be able to assert on its behalf — consistent with `memory:`'s existing rationale ("a
fact about the consumer repo, not about a shareable stack"), even though the underlying
mechanism (relative path resolution) doesn't itself require the restriction.

Refuse instead of ignore-with-diagnostic because this repo already treats a misconfigured
`local:` as an authoring mistake worth stopping the render for (a named directory that
doesn't exist is a hard refuse, not a silent skip) — `local:` on the wrong kind of stack is
the same category of mistake, not a softer one.
