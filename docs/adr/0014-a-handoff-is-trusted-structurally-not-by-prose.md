# A handoff is trusted structurally, not by prose

A **Handoff** drives `implement-handoff`, which dispatches **Implementer**s holding Write, Edit
and Bash. The document therefore chooses a session's tasks, its file boundaries and the command
it runs. It is the second untrusted document this toolkit reads, and it arrives by a different
route from the first: not on a head under review, but in the worktree somebody is working in.

The rule that separates the two cases is that a handoff is written locally and never committed.
One that git tracks arrived with a branch rather than from a session on this machine.

**`git ls-files --error-unmatch` does not implement that rule.** Git tracks *paths*. A branch
that commits a `handoff -> real` symlink alongside `real/task.md` leaves the path
`handoff/task.md` untracked while its content is entirely branch-authored, and the check reports
it as locally written. The rule was sound; the test for it was measuring something adjacent.

`internal/handoff` decides it instead, and `agtk handoff list` is how both entry points ask. It
`Lstat`s the handoff directory before its entries, refuses a symlink rather than resolving one,
measures containment against the resolved directory, and treats a `git ls-files` that declines
to answer — exit 128, a directory that is not a repository — as tracked. Every refusal is named.

This applies ADR 0007 §4 rather than restating it: symlinks are refused, not followed, because
following one makes the property being relied on false. There it is "the reviewed code is a
detached copy". Here it is "this document was written by a session on this machine".

## Considered options

**Add a symlink check to each of the two existing copies.** The obvious repair, and the reason
it is wrong is the reason the defect existed: there were two copies. One is shell in a
SessionStart hook, the other is prose in a skill instructing an agent to run a command. Neither
is a control — the second is a request — and a rule that has to be restated in every caller is
one that will be got right in some of them. It also leaves the next reader of either copy with
no way to tell it is a duplicate.

**Keep it in prose, and make the prose better.** Rejected on ADR 0007's own ground: that ADR
exists because four separate protections are structural rather than described, and it says so in
its title. A handoff decides what a session runs, which is a stronger grant than a review root
gives anyone.

**Refuse committed handoffs at render time instead.** Rejected: the document is written and
committed long after render, on a branch the rendering consumer never sees. There is no moment
at render when the question can be asked.

## Consequences

- **The entry points depend on `agtk`.** The hook fails closed and says so: a missing or failing
  binary withholds the handoff rather than offering it unchecked, because a check that did not
  run is not a check that passed. The cost is that a consumer without `agtk` stops being offered
  handoffs at all, which the note it prints is there to explain.
- **Refusals are reported, never silent.** A document withheld and a directory holding nothing
  produce the same empty list, and only one of them means there is no work waiting. This is the
  same distinction `reviewrun` draws between a reviewer that could not answer and one that found
  nothing.
- **The rendered definitions no longer describe the mechanism**, only that they defer to it and
  what to do when it cannot run. The render tests changed accordingly: they assert delegation and
  the fail-closed path, and which handoffs may be acted on is proved in `internal/handoff`.
- **`internal/memory`'s anchor confinement had the same defect** — lexical, never `Lstat`ing —
  and is fixed alongside. Two instances of one class, and the second was already recorded as a
  memory note; leaving it would have made that note the only thing standing between a repeat.
