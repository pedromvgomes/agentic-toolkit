# Consumer guide

How to wire up a project to consume agentic-toolkit definitions (skills, rules,
instructions, agents, commands, hooks, MCP, settings) and have them rendered
into your editor's native layout (Claude Code today; Cursor, Copilot, OpenCode
on the roadmap).

For the canonical schema reference, see
[`definitions/SCHEMA.md`](../definitions/SCHEMA.md) (definition catalog) and
[`definitions/CONFIG-SCHEMA.md`](../definitions/CONFIG-SCHEMA.md) (stack +
lockfile). This guide is the practical companion — it explains *how to think
about* the stack model, not just *what fields exist*.

## TL;DR

A consumer repo opts in by committing **two files at the repo root**:

- `.agentic-toolkit.yaml` — entry-point **stack manifest**: hand-edited.
- `.agentic-toolkit.lock.yaml` — pinned record of every git source the
  resolver fetched. Resolver-written; **commit it**.

A stack manifest layers on top of one or more imported stacks (`extends:`)
and adds project-local definitions on top (per-category lists). The same
shape is used everywhere — consumer file, shareable stack file, even
nested stack files. There is no "consumer config" vs "preset"
distinction.

## The four kinds of fields

```yaml
# .agentic-toolkit.yaml (or any stack file)
description: optional, only meaningful when this stack is imported
root: ./definitions          # optional, default for bare-name lookups

extends:                     # imported stacks — applied in declared order, later wins
  - github.com/owner/repo.git/stacks/default.yaml@ref
  - ./internal/team-stack.yaml

skills:                      # add definitions on top
  - challenge                # bare → resolved under <root>/skills/challenge/SKILL.md
  - ./local-skills/foo       # path → relative to this stack file's directory
  - github.com/owner/repo.git/skills/baz@ref   # external URL
agents:    [...]
rules:     [...]
instructions: [...]
commands:  [...]
hooks:     [...]
mcp:       [...]
settings:  [...]
```

| Field | Required | Meaning |
|-------|----------|---------|
| `description` | no | One-line summary shown by tooling when this stack is imported. |
| `root` | no | Convention root for bare-name lookups, relative to the stack file's repo root. Default: `definitions`. |
| `extends` | no | Other stacks to layer under this one. Each entry is a URL (with `.git/` boundary) or a local `./path`. Bare names not allowed. |
| `skills` / `agents` / `rules` / `instructions` / `commands` / `hooks` / `mcp` / `settings` | no | Per-category definition lists. Each entry is a URL, a local path, or a bare name. |

## Per-entry resolution

The parser disambiguates each entry string by shape:

- **External URL** — contains `.git/` as the boundary between repo URL
  and in-repo path. Optional `@<ref>` selects a git ref. The resolver
  fetches the repo and locates the bundle/file at the in-repo path.
  Example: `github.com/owner/repo.git/skills/foo@main`.
- **Local path** — starts with `./` or `/`. Resolved relative to the
  directory holding the stack file itself. Example: in a stack at
  `repo/stacks/team.yaml`, `./shared/foo` → `repo/stacks/shared/foo`.
- **Bare name** — anything else. Resolved under
  `<root>/<plural>/<name>...` in the stack file's source FS. The
  default `root` is `definitions`; override per stack file.

Bare names are not permitted in `extends:` — every extends entry must
be either a URL or a `./path`.

## Override semantics

`extends:` is processed depth-first, post-order:

1. The deepest extends are resolved first.
2. Each imported stack's per-category entries are layered into the
   accumulating overlay.
3. The importing stack's own entries apply *after* its extends — so the
   importer always wins on `(category, name)` collisions.
4. Among siblings, later entries override earlier ones.

The entry-point file's own entries always win last.

## Workflow

```bash
agtk init [--extends <url>]    # scaffold .agentic-toolkit.yaml
# … edit the stack file …
agtk sync                      # lock-if-stale + fetch + render in one shot
```

Or the three-step variant for CI:

```bash
agtk lock     # resolve refs to SHAs → .agentic-toolkit.lock.yaml (commit it)
agtk fetch    # hydrate the cache from the lockfile (CI; never resolves refs)
agtk render   # write platform-native files under .claude/ etc.
```

Other commands:

- `agtk plan` — preview what will render without touching disk.
- `agtk status` — drift report between config / lockfile / cache /
  rendered state. Exits non-zero on any drift.
- `agtk lock --frozen` — fail in CI if the lockfile would change.

Re-run `agtk lock` (or `agtk sync`) whenever you change the stack file,
want to bump pinned refs to current heads, or pin a new ref.

## Recipes

### Recipe 1 — extend a single shared stack

You only want what a published stack already gives you:

```yaml
extends:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
```

### Recipe 2 — stack two shared stacks

```yaml
extends:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/bare-repos.yaml@main
```

Last extends wins on conflicts, so layer narrower/opinionated stacks
after broader ones.

### Recipe 3 — extend + add specific definitions from another repo

You like the toolkit's `default` stack, and want to add the `use-gt`
skill and the `worktree-per-session` rule from `pedromvgomes/gt`:

```yaml
extends:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main

skills:
  - github.com/pedromvgomes/gt.git/agentic/skills/use-gt@main
rules:
  - github.com/pedromvgomes/gt.git/rules/worktree-per-session.md@main
```

No need to publish a custom stack to the toolkit repo for this: the
entry-point file *is* a stack and can carry definitions directly.

### Recipe 4 — keep your own definitions next to the consumer file

You have project-local skills you don't want to publish anywhere:

```yaml
extends:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main

skills:
  - ./internal-skills/code-review-style
  - ./internal-skills/perf-profiler
rules:
  - ./internal-rules/no-experimental-apis.md
```

Local skills follow the same bundle layout as published ones
(`<dir>/SKILL.md` with companion files alongside). You pick the folder
— there's no `definitions/` requirement for consumer-side definitions.

If most of your local definitions live under one shared parent, set
`root` to point at it and use bare names instead:

```yaml
root: ./internal-toolkit
extends:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
skills:
  - code-review-style    # → ./internal-toolkit/skills/code-review-style/SKILL.md
  - perf-profiler
```

### Recipe 5 — split your own stack across files

Once your entry-point gets too big, factor parts out into local stacks:

```yaml
# .agentic-toolkit.yaml
extends:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
  - ./stacks/team-style.yaml
  - ./stacks/local-tooling.yaml
```

Each `./stacks/*.yaml` is a stack file with the same shape. They can
extend further, define `root`, and add their own entries.

## Common pitfalls

- **Wrong filename.** It's `.agentic-toolkit.yaml`, with a leading dot
  and `.yaml` (not `.yml`). The lockfile is
  `.agentic-toolkit.lock.yaml`.
- **Forgetting `.git/`.** External URL entries **must** include `.git/`
  as the boundary between the repo URL and the in-repo path. Without
  it, the resolver can't locate the bundle/file.
- **Bare name in `extends:`.** Bare names only resolve to definitions,
  not stacks. Stack imports must use a URL or `./path`.
- **Old-format config.** If you see a `legacy_config` parse error
  pointing at `source:` / `presets:` / `externals:` — that's the
  v1 schema. See [MIGRATION.md](MIGRATION.md) for the upgrade path.
- **Skipping the lockfile.** `agtk plan`, `agtk render`, and `agtk
  fetch` all refuse to run without `.agentic-toolkit.lock.yaml`. Run
  `agtk lock` first (or use `agtk sync`).
