---
description: |
  Rendered agtk output is regenerated, not committed. Determine the repo's policy from
  what git already tracks or ignores before asking, and anchor the .gitignore block to
  the repo root.
---

## Rendered output stays out of git

`agtk render` and `agtk sync` write platform-native output into the repo: `.claude/`,
`CLAUDE.md`, `.agents/`, `AGENTS.md`, `.codex/`, `.mcp.json`. Whether those files are committed
is a per-repo policy decision, and getting it wrong in either direction is expensive — a repo
that commits them collects a rendered diff in every unrelated PR, and a repo that ignores them
loses its whole agent configuration to a `git clean`.

The default policy is: **commit the inputs, ignore the outputs.**

- `.agentic-toolkit.yaml` and `.agentic-toolkit.lock.yaml` are committed. They are the source
  of truth, and the lockfile is what makes a regeneration reproducible.
- `.gitignore` itself is committed — it is never one of the ignored files, even though the
  block it carries is about them.
- The rendered output is gitignored and regenerated with `agtk render` (or `agtk sync` when a
  remote source also needs refreshing). Never hand-commit a rendered file to "fix" it.

Match this repo's own root `.gitignore` block, comment included:

```gitignore
# Agentic-toolkit generated outputs — rendered from .agentic-toolkit.yaml (and
# the stacks it extends) on every toolkit sync. The config and lockfile are
# committed; these regenerate, so they are never committed. Anchored to the
# repo root so intentionally tracked CLAUDE.md files in subdirectories are
# unaffected.
/.claude/
/CLAUDE.md
/.mcp.json
/.codex/
/AGENTS.md
/.agents/
```

The leading `/` is load-bearing. A bare `.claude/` or `CLAUDE.md` pattern matches at every
depth, so it silently swallows a hand-written `CLAUDE.md` in a subdirectory that the repo
tracks on purpose. Anchor every one of these patterns to the repo root.

### Determining a repo's policy

An always-on instruction has nowhere to remember an answer, so do not treat "ask the user"
as the first step. Read the policy off the repo, in this order, and stop at the first signal:

1. `git ls-files -- .claude CLAUDE.md .agents AGENTS.md .codex .mcp.json` — if anything comes
   back, this repo commits its rendered output. Follow that policy: commit the regenerated
   files alongside the config change that caused them.
2. Otherwise `git check-ignore -v .claude/ CLAUDE.md .agents/ AGENTS.md .codex/ .mcp.json`, or
   read the root `.gitignore` — if they are ignored, the repo follows the default policy.
   Leave the rendered files out of every commit.
3. Only when neither signal exists is the policy genuinely unestablished. Ask the user once
   which one applies, and when they choose the default, write the root-anchored block above
   into the root `.gitignore`.

### Keeping ignored output fresh

A repo on the default policy has no rendered output after a fresh clone until something runs
`agtk render`. A `SessionStart` hook in the **user-level** `~/.claude/settings.json` can do that
on every session start, which keeps every such repo current without any per-repo setup.

Run `agtk render`, not `agtk sync`, from that hook. `.agentic-toolkit.lock.yaml` is committed
under the default policy, so `render` has everything it needs and never resolves a ref over the
network; `sync` relocks whenever the lockfile looks stale, which means a hook wired to it
resolves mutable refs like `@main` live and writes whatever hooks and MCP servers *that ref
currently points to* into `.claude/settings.json` and `.mcp.json`, without anyone having looked
at what changed since the lockfile was last updated.

Choosing `render` over `sync` removes only that live-resolution step. It does not make an
unreviewed repo safe to open: `render` still writes whatever hooks and MCP servers the repo's
own committed manifest, lockfile and `agentic/hooks/`/`agentic/mcp/` name, verbatim, into
`.claude/settings.json` and `.mcp.json` — a user-level hook fires in every repo it opens, and
neither command distinguishes a repo you have reviewed from one you have not. Trusting a repo's
committed automation is a separate decision, one the platform's own workspace-trust prompt is
for, not something this hook choice settles.

It has to be user-level. A per-repo hook is configured inside `.claude/`, which is exactly the
gitignored tree the hook would need to exist in order to bootstrap — after a fresh clone there
is no hook to run.

### Relationship to "Instructions are rendered, not edited"

That instruction covers *editing* rendered files (don't — the next render overwrites the
edit); this one covers whether those files are *committed* at all. Separate axes, so both
apply at once: a repo that commits its rendered output still edits the source definition
rather than the generated file.
