# Reference: adding and importing definitions

Procedures for the three jobs this skill covers — adding your own definition, importing someone
else's, authoring a local stack — plus the collision rules that decide what actually renders.

`docs/CONSUMER-GUIDE.md` in the agentic-toolkit repo is the wider document (rendering targets,
memory configuration, `--config`/`--source`); this file is scoped to changing what a consumer
repo ships.

## 1. Add your own definition (convention scan)

Drop the file under `<root>/<category>/` and run `agtk render`. Nothing names it anywhere:
`root:` defaults to `agentic`, and each category has one fixed directory under it.

| Category | Path under `<root>/` | Shape |
|---|---|---|
| skill | `skills/<name>/SKILL.md` | one subdirectory per skill; companion files sit beside `SKILL.md` |
| agent | `agents/<name>/AGENT.md` | one subdirectory per agent |
| rule | `rules/<name>.md` | flat `*.md` |
| instruction | `instructions/<name>.md` | flat `*.md` |
| command | `commands/<name>.md`, nested allowed | `*.md` at any depth; nesting becomes namespacing |
| hook | `hooks/<name>.yaml` | flat `*.yaml` / `*.yml` |
| mcp | `mcp/<name>.yaml` | flat `*.yaml` / `*.yml` |
| setting | `settings/<name>.yaml` | flat `*.yaml` / `*.yml` |

```
agentic/
├── skills/code-review-style/SKILL.md      # skill "code-review-style"
├── skills/code-review-style/checklist.md  # companion, copied verbatim
├── agents/perf-profiler/AGENT.md          # agent "perf-profiler"
├── rules/no-experimental-apis.md          # rule "no-experimental-apis"
├── instructions/house-style.md            # instruction "house-style"
├── commands/db/reset.md                   # command "db/reset" → /db:reset on Claude
├── hooks/log-tools.yaml
├── mcp/context7.yaml
└── settings/deny-dangerous-shell.yaml
```

Anything the shape does not cover is ignored, not failed — a `README.md` beside the hooks is not
a broken definition. A skill or agent is a *directory*; a loose `agentic/skills/foo.md` is
invisible to the scan.

### Names

For a skill or an agent, the name is the directory name, and a `name:` in frontmatter must equal
it. For the file-shaped categories, a `name:` in frontmatter wins and the filename is only the
fallback — so `agentic/instructions/style.md` declaring `name: house-style` renders as the
instruction `house-style`.

### What is and is not an error

- **A missing category directory is an empty one.** You do not create seven empty directories to
  use the eighth.
- **A missing `context:` file is a hard error.** `context:` names one file of free-form repo
  prose (rendered as the instruction `context`); naming a file this repo does not have fails the
  resolve.
- **Two files in one scanned category declaring the same `name:` is a hard error**, naming both
  files. Which one you meant is not knowable, and dropping one silently would render an
  incomplete repo. Every category is still scanned and every file still parsed, so one run
  reports all of the problems at once.

## 2. Import specific definitions from another repo

An entry manifest has no per-category lists, so one-off imports go through a small local stack:

```yaml
# stacks/extras.yaml
skills:
  - github.com/pedromvgomes/gt.git/agentic/skills/use-gt@main
rules:
  - github.com/pedromvgomes/gt.git/rules/worktree-per-session.md@main
mcp:
  - github.com/someorg/tools.git/definitions/mcp/sentry.yaml@v1.2.0
```

```yaml
# .agentic-toolkit.yaml
stacks:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
  - ./stacks/extras.yaml
```

Then `agtk lock && agtk render` (or `agtk sync`) — the URL entries need pins.

URL entries must contain `.git/` as the boundary between the repo URL and the in-repo path, and
the in-repo path points at the *file* for a file-shaped category and at the *bundle directory*
for a skill or an agent. `@ref` is optional; see the gotcha on empty refs below.

To take a whole published stack instead, name it directly under `stacks:` — entries there are
applied in order and later ones win.

## 3. Author or extend a local stack

A local stack is an ordinary stack file that happens to live in this repo and is reached by
`./path`. It never needs publishing.

```yaml
# stacks/team.yaml
description: Team conventions layered over the shared default stack.
extends:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
root: ./definitions          # where bare names resolve; default is `definitions`
skills:
  - challenge                # bare → <root>/skills/challenge/SKILL.md in this repo
  - ./local-skills/foo       # path → relative to this stack file's directory
instructions:
  - house-style
```

Three constraints, all of them consequences of a stack being its own type:

- `root:`, `context:`, `memory:` and `platforms:` **do not exist on a stack**. Setting `memory:`
  or `platforms:` at the top level of a stack is a parse error that names the field and points at
  the entry manifest. (A stack's `root:` above is a *different* field with a different meaning:
  where bare names resolve, defaulting to `definitions`. The entry manifest's `root:` names the
  convention-scan root and defaults to `agentic`.)
- Bare names are valid only in a stack's per-category lists — never in `extends:` or `stacks:`,
  where every entry is a URL or a `./path`.
- A bare name resolves inside the stack file's *own* source, so a bare name in a stack you
  fetched from another repo means a definition in that repo, not in yours.

## 4. Gotchas

**Your own scanned content wins every collision.** The entry manifest's `stacks:` are walked
first, its convention root is scanned after, and the winner for a `(category, name)` is whatever
merged last. Locally-scanned content therefore overrides anything a stack contributed, without
any rule specific to it — it is the ordinary last-wins overlay plus the visit order. An override
is reported as a diagnostic, not an error.

**A file-shaped override collides on the frontmatter `name:`, not on the entry you wrote.** For
rule, instruction, command, hook, mcp and setting, the name comes from the file's own
frontmatter (falling back to the filename). So a local `agentic/instructions/style.md` intended
to replace an upstream instruction whose frontmatter says `name: house-style` does not replace
it — the two have different names and both render. Match the upstream `name:` to override it.
Skills and agents are not affected: their name is the bundle directory's name, and frontmatter
must agree with it.

**Source URLs are matched byte-for-byte.** The lockfile is keyed on the exact `(URL, ref)`
strings, with no canonicalization — adding or dropping a `https://` prefix, a trailing slash or
a `www.` is a different source, so re-spelling a locked URL always means another `agtk lock`.

**An empty `ref:` resolves only when the URL has exactly one pin.** Omitting `@ref` makes the
lookup fall back to the unique lockfile pin for that URL; two pins for the same URL under
different refs are ambiguous and refused rather than guessed at.

**On the `codex` platform only, a skill and a command sharing a name collide.** Both resolve to
`.agents/skills/<name>/SKILL.md`; the skill wins and the command is dropped from the Codex
render, reported on stdout rather than raised as an error. Claude Code keeps `.claude/skills/`
and `.claude/commands/` apart, so the same pair renders fine there — which is why the collision
is a skip and not a failure.

**`.agtk-manifest.json` is what makes a render safe to re-run.** The sidecar records every
whole-owned file a render wrote (skills, agents, commands, rules) with its content hash. A file
in it is overwritten freely; a file present on disk but *absent* from it makes the render refuse
with `exists and is not tracked by agtk; rerun with --force to overwrite`. Deleting the manifest
does not reset anything — it escalates every previously-rendered file to untracked, so the next
render refuses on all of them.

**`CLAUDE.md` has a managed region.** `agtk` owns only what sits between
`<!-- BEGIN AGTK MANAGED -->` and `<!-- END AGTK MANAGED -->`; content outside the markers
survives verbatim, and a hand-edit inside them is overwritten by the next render with no warning.
A `CLAUDE.md` with no markers gets the block appended; a missing one is created with just the
block. To change what is inside the region, change an instruction definition. Under the `codex`
platform `AGENTS.md` is `agtk`'s file outright, regenerated from `instructions:` plus an index of
`rules:` on every render.

**Rendered trees are not places to commit into.** `.claude/`, `.agents/` and `.codex/` belong to
their platform. Keep your own content — definitions under `root:`, the memory store — outside
them.
