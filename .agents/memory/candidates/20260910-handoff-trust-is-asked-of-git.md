---
about: which handoff documents a session may act on is decided by asking git what is untracked, not by comparing paths — because git's path comparison folds case, pathspecs and Unicode composition, and each fold missed hands a branch-authored document to subagents holding Write and Bash
saw:
  - internal/handoff/handoff.go
  - internal/cli/handoff.go
  - definitions/hooks/handoff-claude-session-start.yaml
  - definitions/skills/implement-handoff/SKILL.md
  - docs/adr/0014-a-handoff-is-trusted-structurally-not-by-prose.md
---

A **Handoff** drives `implement-handoff`, which dispatches subagents holding Write, Edit and
Bash, so the document chooses a session's tasks, file boundaries and commands. The rule
separating a local one from a branch-authored one is that a handoff is written locally and never
committed.

`internal/handoff.List` is the only place that decides it, and `agtk handoff list` is how the
session-start hook and `implement-handoff` both ask. Neither implements the check any more; ADR
0014 records why patching the two copies was rejected.

**The tracked/untracked question is put to git, not answered by comparing strings.**
`untrackedNames` runs `git ls-files -z -o -- handoff` and refuses anything git does not name.
Doing the comparison here means reimplementing git's own, which has more folds in it than it
looks, and each one missed reads a branch-authored document as locally written. All four were
reachable and each was found only after the previous fix closed the one in front of it:

- the **directory** name, where a case-insensitive filesystem answers `handoff/` for a committed
  `Handoff/` while git's index stays case-sensitive;
- the **filename**, where a checkout of `handoff/Task.md` over an existing `task.md` leaves the
  on-disk name lowercase;
- the **pathspec** used to ask, which is itself matched case-sensitively — `-- handoff` returned
  nothing against an index holding `Handoff/task.md`, so the whole directory read as untracked;
- **Unicode composition**, where the filesystem stores a name decomposed and the index holds it
  composed (`core.precomposeunicode`).

Two flags are load-bearing and neither is obvious:

- **`--exclude-standard` must be absent.** `handoff/` is excluded through the repository's
  `info/exclude`, so applying the ignore rules hides every legitimate handoff and the feature
  reports nothing, always. Ignored-and-untracked is the state a handoff lives in.
- **The pathspec must be present.** Without it the command walks the whole worktree with ignore
  rules disabled, enumerating every ignored path, on every session start. It is matched against
  worktree paths — spelled as the directory listing spells them — so it does not reintroduce the
  index-side folding this delegates.

Structure decides the rest, per ADR 0007's principle that a symlink is refused rather than
followed: the handoff directory is `Lstat`ed before its entries, a symlinked directory or entry
is refused, a nested repository is refused (a committed submodule holds real files whose paths
the outer index does not carry), and a directory reachable only by case-folding is refused
because the name is what git's index is keyed by.

Two fail-closed choices: a git that will not answer offers nothing, and paths are printed with
`%q` because the hook pipes this output into a fresh session's context and a filename carrying
newlines would otherwise write its own lines there.

Tests that assert a fold must skip where the filesystem does not fold — on a case-sensitive
filesystem `Task.md` and `task.md` are two different files, the untracked one is genuinely local,
and offering it is correct.
