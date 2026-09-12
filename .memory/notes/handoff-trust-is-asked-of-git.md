---
name: handoff-trust-is-asked-of-git
kind: invariant
description: Whether a handoff is locally written is decided by asking git what is untracked, never by comparing paths, because git's path comparison folds case, pathspecs and Unicode composition.
anchors:
  - path: source/toolkit/internal/handoff/handoff.go
    blob: 9c006dc4dd34
  - path: source/toolkit/internal/cli/handoff.go
    blob: d1d9642d265c
  - path: definitions/hooks/handoff-claude-session-start.yaml
    blob: d5d8a261ef6f
  - path: definitions/skills/implement-handoff/SKILL.md
    blob: df2845a8f6a1
  - path: docs/adr/0014-a-handoff-is-trusted-structurally-not-by-prose.md
    blob: 916a825b0b86
confidence: verified
---

A handoff drives `implement-handoff`, which dispatches subagents holding Write, Edit and Bash,
so the document chooses a session's tasks, file boundaries and commands. The rule separating a
local one from a branch-authored one is that a handoff is written locally and never committed.

`handoff.List` (`source/toolkit/internal/handoff/handoff.go:59`) is the only place that decides it, and
`agtk handoff list` is how both callers ask — the session-start hook
(`definitions/hooks/handoff-claude-session-start.yaml:36`) and the skill
(`definitions/skills/implement-handoff/SKILL.md:34`). Neither implements the check; ADR 0014
records why patching two copies was rejected.

**The tracked/untracked question is put to git, not answered by comparing strings.**
`untrackedNames` (`handoff.go:188`) runs `git ls-files -z -o -- handoff` (`:194`) and refuses
anything git does not name. Doing the comparison here means reimplementing git's own, which
has more folds in it than it looks, and each one missed reads a branch-authored document as
locally written. All four were reachable and each was found only after the previous fix closed
the one in front of it: the **directory** name, where a case-insensitive filesystem answers
`handoff/` for a committed `Handoff/`; the **filename**, where a checkout of `handoff/Task.md`
over an existing `task.md` leaves the on-disk name lowercase; the **pathspec** used to ask,
itself matched case-sensitively, so `-- handoff` returned nothing against an index holding
`Handoff/task.md` and the whole directory read as untracked; and **Unicode composition**, where
the filesystem stores a name decomposed and the index holds it composed
(`core.precomposeunicode`).

Two flags are load-bearing and neither is obvious (`handoff.go:179-187, 189-193`):

- **`--exclude-standard` must be absent.** `handoff/` is excluded through the repository's
  `info/exclude`, so applying the ignore rules hides every legitimate handoff and the feature
  reports nothing, always. Ignored-and-untracked is the state a handoff lives in.
- **The pathspec must be present.** Without it the command walks the whole worktree with ignore
  rules disabled, enumerating every ignored path, on every session start. It is matched against
  worktree paths — spelled as the directory listing spells them — so it does not reintroduce
  the index-side folding this delegates.

Structure decides the rest, per ADR 0007's principle that a symlink is refused rather than
followed: the handoff directory is `Lstat`ed before its entries (`:82`), a symlinked directory
or entry is refused (`:47-49`, `:145`), a nested repository is refused because a committed
submodule holds real files whose paths the outer index does not carry (`:96-102`), and a
directory reachable only by case-folding is refused because the name is what git's index is
keyed by (`hasExactly`, `:225`).

Two fail-closed choices: a git that will not answer offers nothing (`:196-199`), and paths are
printed with `%q` (`source/toolkit/internal/cli/handoff.go:86,89`) because the hook pipes this output into a
fresh session's context and a filename carrying newlines would otherwise write its own lines
there.

Tests that assert a fold must skip where the filesystem does not fold — on a case-sensitive
filesystem `Task.md` and `task.md` are two different files, the untracked one is genuinely
local, and offering it is correct.
