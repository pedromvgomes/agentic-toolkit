# Consumer guide

How to wire up a project to consume agentic-toolkit definitions (skills, rules,
instructions, agents, commands, hooks, MCP, settings) and have them rendered
into your editor's native layout (Claude Code today; Cursor, Copilot, OpenCode
on the roadmap).

For the canonical schema reference, see
[`definitions/SCHEMA.md`](../definitions/SCHEMA.md) (definition catalog) and
[`definitions/CONFIG-SCHEMA.md`](../definitions/CONFIG-SCHEMA.md) (entry
manifest + stack + lockfile). This guide is the practical companion — it
explains *how to think about* the model, not just *what fields exist*.

## TL;DR

A consumer repo opts in by committing **two files at the repo root**:

- `.agentic-toolkit.yaml` — the **entry manifest**: hand-edited.
- `.agentic-toolkit.lock.yaml` — pinned record of every git source the
  resolver fetched. Resolver-written; **commit it**.

Configuration for one part of `agtk` goes under `.agentic-toolkit/` instead.
Content you commit — a memory store, local definitions under `root:` — lives
where your entry manifest names it, in neither place. See **Toolkit
namespace** in [`CONTEXT.md`](../CONTEXT.md) for which a given file is.

The entry manifest composes one or more shared **stacks** (`stacks:`) and
finds the rest of your own content by convention: anything you drop under
`<root>/<category>/` (default `root` is `agentic`) renders without being
named in the manifest. A stack — the shareable unit published at
`stacks/*.yaml` in any repo, including this one — is a different, related
shape: it uses `extends:` to layer other stacks and lists definitions by
name per category, because a stack is meant to be imported into many repos
rather than scanned out of one repo's tree.

## Entry manifest fields

```yaml
# .agentic-toolkit.yaml
description: optional, only meaningful if this file is ever imported as a stack
root: agentic                # convention root for your own content; default shown
context: ./CONTEXT.md        # optional, one file describing this repo to tooling

stacks:                       # imported stacks — applied in declared order, later wins
  - github.com/owner/repo.git/stacks/default.yaml@ref
  - ./internal/team-stack.yaml

platforms:                    # optional, defaults to [claude]
  - claude
  - codex

memory:                        # optional, defaults shown
  root: .memory
```

| Field | Required | Meaning |
|-------|----------|---------|
| `description` | no | One-line summary, read only if this file is itself imported as a stack. |
| `root` | no | Convention root under which your own content is scanned by category directory. Default: `agentic`. |
| `context` | no | Path to one file of free-form repo description, surfaced to tooling that needs to describe the consumer. |
| `stacks` | no | Shared stacks to compose into this entry manifest. Each entry is a URL (with `.git/` boundary) or a local `./path`. Bare names not allowed. |
| `platforms` | no | Rendering targets. Defaults to `[claude]`. See **Rendering targets** below. |
| `memory` | no | Where the repo-resident memory store lives and which agent curates it. See **Memory store** below. |

There is no per-category list (`skills:`, `instructions:`, …) here — content
you own is found by convention scanning instead.

## Convention scanning

Your own skills, instructions, rules, agents, commands, hooks, MCP servers
and settings live under `<root>/<category>/` (default `root` is `agentic`),
one directory per category: `agentic/skills/`, `agentic/instructions/`,
`agentic/rules/`, `agentic/agents/`, `agentic/commands/`, `agentic/hooks/`,
`agentic/mcp/`, `agentic/settings/`. Drop a bundle there and it renders — you
never name it in `.agentic-toolkit.yaml`.

This repo's own entry manifest is a worked example:
[`.agentic-toolkit.yaml`](../.agentic-toolkit.yaml) composes shared stacks
under `stacks:`, and
[`agentic/instructions/agentic-toolkit-repo.md`](../agentic/instructions/agentic-toolkit-repo.md)
renders because it sits under `agentic/instructions/`, without being listed
anywhere.

The `using-agentic-toolkit` skill
(`definitions/skills/using-agentic-toolkit/`) covers the same ground —
adding local definitions, importing definitions from other repos, authoring
local stacks — packaged for an agent working inside a consumer repo to
consult inline rather than for a human reading this guide.

## Stack fields

A stack — a file at `stacks/*.yaml` in any repo, meant to be shared through
an entry manifest's `stacks:` or another stack's `extends:` — keeps the
per-category-list shape:

```yaml
# stacks/my-team.yaml (or any stack file)
description: optional, shown by tooling when this stack is imported
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

Entries in a stack's `extends:` and per-category lists, and in an entry
manifest's `stacks:`, are strings the parser disambiguates by shape:

- **External URL** — contains `.git/` as the boundary between repo URL
  and in-repo path. Optional `@<ref>` selects a git ref. The resolver
  fetches the repo and locates the bundle/file at the in-repo path.
  Example: `github.com/owner/repo.git/skills/foo@main`.
- **Local path** — starts with `./` or `/`. Resolved relative to the
  directory holding the file itself. Example: in a stack at
  `repo/stacks/team.yaml`, `./shared/foo` → `repo/stacks/shared/foo`.
- **Bare name** — anything else. Only valid in a stack's per-category
  lists, resolved under `<root>/<plural>/<name>...` in the stack file's
  source FS. The default `root` is `definitions`; override per stack file.

Bare names are not permitted in `extends:` or in `stacks:` — every entry
there must be either a URL or a `./path`. An entry manifest has no
per-category lists to resolve a bare name against; its own content is
found by convention scanning instead (see above).

## Override semantics

`extends:` (and an entry manifest's `stacks:`) is processed depth-first,
post-order:

1. The deepest imports are resolved first.
2. Each imported stack's per-category entries are layered into the
   accumulating overlay.
3. The importing file's own entries — a stack's per-category lists, or an
   entry manifest's convention-scanned content — apply *after* its
   extends/stacks, so the importer always wins on `(category, name)`
   collisions.
4. Among siblings, later entries override earlier ones.

The entry-point file's own entries always win last.

### `permissions` composes instead

Settings definitions resolve the same way — last stack wins the whole
top-level key — with one exception. A settings definition's `permissions`
is merged rather than replaced: `allow`, `deny` and `ask` union across
every definition that contributes them, deduplicated on the exact rule
string.

Without that, the key has one owner per render, and a stack that bundles
an agent has nowhere to put that agent's grants — contributing them takes
the whole allow list away from every stack it was layered with, silently.
Composition is what lets a stack ship the pre-approvals for the
definitions it ships.

So a settings definition of your own **adds** to the grants your stacks
contribute; it does not take the key back. To narrow, name the rule
again on the side you want:

- `deny` refuses it outright. Claude Code resolves `deny` ahead of
  `allow`, so it wins over any grant a stack contributed.
- `ask` turns a stack's pre-approval back into a prompt, which is what
  you want for something you mean to allow sometimes rather than never.

Both compose the same way `allow` does, so a stack you extend cannot
drop the rule you added.

Every other key — `model`, `env` — is still last-wins, and your
entry-point file still wins it.

## Workflow

```bash
agtk init [--stacks <url>]     # scaffold .agentic-toolkit.yaml
# … edit the entry manifest …
agtk sync                      # lock-if-stale + fetch + render in one shot
```

Or the three-step variant for CI:

```bash
agtk lock     # resolve refs to SHAs → .agentic-toolkit.lock.yaml (commit it)
agtk fetch    # hydrate the cache from the lockfile (CI; never resolves refs)
agtk render   # write platform-native files for each opted-in platform
```

Other commands:

- `agtk plan` — preview what will render without touching disk.
- `agtk status` — drift report between config / lockfile / cache /
  rendered state. Exits non-zero on any drift.
- `agtk lock --frozen` — fail in CI if the lockfile would change.

Re-run `agtk lock` (or `agtk sync`) whenever you change the entry manifest,
want to bump pinned refs to current heads, or pin a new ref.

## Rendering targets

`agtk render` writes Claude Code's layout and nothing else unless you say
otherwise. Add `platforms:` to your entry manifest to render additional
targets from the same definitions:

```yaml
stacks:
  - https://github.com/you/your-toolkit.git/stacks/default.yaml@main
platforms:
  - claude
  - codex
```

There are two `platforms:` fields and they answer different questions.
This one, on the stack, names the platforms a render *writes for*. The
one on an individual definition narrows the platforms *that definition*
is meant for, and a definition that omits it goes to all of them — so a
hook shelling out to a Claude-only binary, or a settings fragment in
Claude's `permissions` vocabulary, declares `platforms: [claude]` and is
skipped when the Codex adapter runs. Categories that carry context
rather than wiring — `instructions:`, `skills:`, `rules:`, `agents:`,
`commands:` — are usually meant for every platform and should leave the
field off.

Omitting `platforms:` is the same as `[claude]`. This lives in the entry
manifest rather than behind a `--platform` flag so that every render site —
your shell, a hook, CI, a colleague's checkout — reads the same answer
out of something committed. Naming a platform with no render adapter
fails the render rather than writing nothing.

Where each category lands, under the default `--scope project`:

| Category | `claude` | `codex` |
|---|---|---|
| `skills` | `.claude/skills/<name>/SKILL.md` | `.agents/skills/<name>/SKILL.md` |
| `commands` | `.claude/commands/<name>.md` | converted to a skill at `.agents/skills/<name>/SKILL.md` |
| `agents` | `.claude/agents/<name>/AGENT.md` | `.codex/agents/<name>.toml` |
| `rules` | `.claude/rules/<name>.md` | `.agents/rules/<name>.md`, indexed in `AGENTS.md` |
| `instructions` | `CLAUDE.md` managed region | `AGENTS.md` |
| `mcp` | `.mcp.json` | `.codex/config.toml` |
| `settings` | `.claude/settings.json` | `.codex/config.toml` |
| `hooks` | `.claude/settings.json` | `.codex/config.toml` |

Under `--scope user` the same layouts are written beneath your home
directory, with one exception: `mcp:` renders nothing for `claude`,
because Claude Code keeps user-scoped MCP servers in `~/.claude.json`
under a per-project map rather than in a `.mcp.json` of its own.

Where a definition is *almost* portable, an extension block carries the
difference rather than a second definition. An MCP server told which
client it is serving takes a different argv under each, so it declares
`extensions.codex.args` and stays one definition — splitting it would
rename it, since the definition's name is the name the server is
addressed by.

Three things to know before opting into `codex`:

- **`AGENTS.md` becomes agtk's file.** It is rewritten from your
  `instructions:` on every render, plus an index of your `rules:` —
  Codex has no rules-discovery mechanism, so without the index a rule is
  written where nothing reads it. If you already have a hand-authored
  `AGENTS.md`, the first render refuses until you pass `--force`, so move
  anything you want to keep into an `instruction` definition first.
  There is no longer any path by which an `AGENTS.md` you wrote reaches
  `CLAUDE.md`: agtk owns the one and generates the other.
  `CLAUDE.md` is unaffected: the two files are built independently from
  the same `instructions:`, and neither reads the other.
- **A command and a skill sharing a name collide on Codex**, since both
  land at `.agents/skills/<name>/SKILL.md`. The skill wins and the
  command is skipped, with a line on stdout saying so. Nothing changes on
  Claude, where the two have separate destinations.
- **A `prompt` hook handler and an `sse` MCP transport are skipped**, and
  reported on stdout — by a dry run as well as a real render, so
  `--dry-run` shows you what a render would leave out. Codex parses a
  prompt handler but never runs it, and has no sse client. The same
  definitions still render for Claude in the same pass. A hook's
  `fail_closed` is dropped without a line of its own: a Codex hook
  blocks by the exit code it returns, so there is no key for it to go
  to.

`.agents/` is agtk's output for this platform, so nothing you maintain by
hand belongs inside it: a repo ignores a rendered root wholesale, and that
ignore rule reaches whatever is committed underneath. The memory store's
default sits outside it for exactly this reason — see **Toolkit namespace**
in [CONTEXT.md](../CONTEXT.md).

## Memory store

`agtk memory` manages a repo-resident store of durable notes about your
codebase — invariants, rationale, gotchas and dead ends that cost real
exploration to learn. It defaults to `.memory/` next to your entry manifest,
is committed, and is reviewed in PRs like any other source. Set
`memory.root` to put it somewhere else; keep that somewhere outside every
platform's rendered tree, for the reason **Toolkit namespace** gives in
[CONTEXT.md](../CONTEXT.md).

A store scaffolded under an older `agtk`, at `.agents/memory/`, is reported
rather than read as a repo with no notes. Move it with `git mv .agents/memory
.memory`, or set `memory.root: .agents/memory` to leave it where it is.

Wherever you put it, the pre-approved permissions for reading the index and
staging candidates follow it: the `claude` adapter builds them from the root
you configured and appends them to `permissions.allow`, so moving the store
does not leave the explorer prompting on every delegation. They are appended
only where your stack already pre-approves something — a deny-only
`permissions` block is left as it is.

```bash
agtk memory index               # regenerate INDEX.md (scaffolds the store)
agtk memory anchor              # stamp blob hashes into note anchors
agtk memory audit               # report notes whose anchored files changed
agtk memory lint                # structural check for CI, notes and candidates
agtk memory search [--files a,b] [words…]
                                # rank notes against files and words, count no read
agtk memory show <name>         # read one note, and count the read
agtk memory stats               # size, staleness, hit rate
agtk memory hits fold [--check] # move the local read log into a committed shard
agtk memory candidates          # list the findings staged for curation
agtk memory curate              # rule on the staged candidates (calls a model)
```

Every command takes `--json`. Nothing here calls a model except `curate`:
every other subcommand is deterministic, so they are safe in hooks and CI.

`search` is how the explorer finds the notes that bear on a question. It ranks
them by the files and words you name, with a note anchoring a named file above
any that matches on words alone, and it scans the notes on every call, so a
result always reflects what is on disk. It records no read and marks a stale
note without demoting it; open a result with `show`, which is the read that
counts. See `docs/adr/0023-memory-retrieval-is-a-file-scan-behind-a-ranker.md`.

`stats` also reports where the store is, which is how an agent finds it
without re-deriving resolution from the manifest:

```json
{ "root": ".memory", "project_root": "." }
```

`root` is where notes and candidates live — `memory.root` if the entry
manifest sets one. `project_root` is the repo that anchor paths are
relative to. They are normally different directories, though
`memory.root: .` collapses them; either way, resolve an anchor against
`project_root` and never against `root`. What `--source` changes is where
each is derived from — the store belongs to the consumer, not to the
source tree.

Both are relative to the working directory when they sit below it, and
absolute otherwise. Do not read `memory.root` out of the manifest instead:
`--source` changes where `root` is derived from, so a value read out of
the manifest by hand can name a different directory than the one `agtk`
uses.

A note is a markdown file with frontmatter:

```markdown
---
name: lockfile-pins-shas-not-tags
kind: invariant
description: Lock resolution pins commit SHAs, never tags.
anchors:
  - path: internal/resolver/graph.go
  - path: internal/lockfile/*.go
confidence: verified
---

`agtk lock` resolves `extends:` graphs to commit SHAs, never tags — see
`internal/resolver/graph.go:88`.
```

Write the `path:` entries; `agtk memory anchor` fills in the hashes and
expands the globs. Anchor paths are relative to the directory holding your
entry manifest, stay inside it, and are one directory level deep — `**` is
rejected rather than silently truncated.

`audit` never writes: staleness is recomputed from your working tree on
every run, so `confidence:` stays your own judgment about whether the claim
was checked. `lint` is the CI command, and it deliberately passes on a
stale store — a rename in an unrelated PR should not go red. It also reports
each staged candidate whose frontmatter does not parse, with the file and
the YAML error, and fails on it: such a candidate is otherwise dropped from
the backlog in silence. An unquoted colon inside `about:` is the usual cause.

### Curating candidates

`agtk memory curate` runs the curator over the candidates staged in
`candidates/`, or over stale notes with `--stale`, through the provider that
`memory.agent` names in the entry manifest. It is the one memory command that
calls a model, and it has no default provider. Naming notes scopes the run to
them and the candidates that target them, `--limit N` points it at the oldest
`N` candidates, and `--dry-run` reports what it would do and writes nothing.

When the run ends, `agtk` checks the curator's completion report against the
store, in both directions: what the report claims happened did, and everything
that happened is in the report. Only if that check passes does `agtk` itself
delete every candidate the report names as resolved that is still in
`candidates/`, and print each one as a `cleared:` line (a `cleared` list under
`--json`). No model is involved in that step, and nothing is cleared when the
check fails or under `--dry-run`. A candidate the curator already deleted is
simply not listed.

`curate` exits non-zero when:

- the completion report does not match the store. Nothing is rolled back and
  nothing is cleared, so the store is as the run left it.
- the run is the backlog job — no notes named, no `--limit`, no `--stale`, no
  `--dry-run` — and a readable candidate is still in `candidates/` without
  having been reported resolved. A scoped, limited, stale or dry run is not
  expected to rule on every candidate, so a candidate that remains there is
  not an error.
- a candidate in `candidates/` cannot be parsed. Each is listed at the end of
  the run as an `unreadable:` line (an `unreadable` list under `--json`), and
  is never cleared. The curator cannot repair one, so it is yours to fix;
  `agtk memory lint` names the file and the error.

To put the store somewhere else, set it in your entry manifest:

```yaml
memory:
  root: docs/memory
```

`memory:` is an entry-manifest field only — a stack file has no such key,
so one that sets `memory:` fails to parse. Where your repo commits its
notes is not a shared stack's business.

### Hit counts

`show` appends each read to `<root>/.hits.jsonl`, which is gitignored and so
belongs to one checkout. `agtk memory hits fold` writes those reads to one
committed shard, `<root>/hits/<date>-<branch>-<suffix>.json`, holding each
note's read count and its first and last read, and only then empties the
local log; a fold that cannot write the shard leaves the log as it was. It
never commits: the shard is left in the working tree for your own commit.
Every fold names a file no other fold names, so branches that both fold do
not conflict. With `--check` it writes nothing and exits non-zero while the
local log holds reads that are not folded, and an empty or missing log writes
nothing.

`curate` compacts the shards into `<root>/hits.json` once its completion
report has passed the check above, on every real run — backlog, `--stale`,
scoped or limited — and never under `--dry-run` or when the check fails. The
file is written through a temporary file and a rename, then the folded shards
are removed, and no model is involved. A shard that cannot be read is left in
place and reported as a `hits:` line; a `hits.json` that cannot be read fails
the run and leaves the shards alone. A merge conflict in `hits.json` is
resolved by taking either side, and the reads only the other side held are
lost.

`stats` reads the compacted file, the shards and the local log together. The
hit rate is still the distinct notes read over the notes, and a read counts
only for a note that still exists. `cold` lists every note with no read
anywhere in that union, and the window runs from its earliest to its latest
read. While no shard or compacted file exists, the count is labelled as this
checkout's alone; once one does, only the reads not yet folded are, on an
`unfolded:` line. Reads in a session that never folds, an unfolded
`git checkout`, and a renamed note's earlier reads are not counted. See
`docs/adr/0024-hit-counts-are-carried-in-committed-shards.md`.

`open-pr` folds for you. It probes `agtk memory hits --help` and skips the
fold when the installed `agtk` lacks it, folds before it commits the memory
changes so the shard lands in that commit, and checks that nothing under the
store root is left uncommitted before it pushes. `/memory-curate` carries no
such check.

## Cloud sessions

A **Cloud session** commits as the platform's own identity and signs through the platform's
signer. `agtk cloud init` replaces that with yours, from three environment variables:

| Variable | Effect |
|---|---|
| `AGTK_GH_USER` | git `user.name` |
| `AGTK_GH_EMAIL` | git `user.email` |
| `AGTK_SIGNING_KEY_B64` | optional base64 OpenSSH private key, no passphrase; commits and tags are signed with it |

The identity is written globally and into each checkout that has no repo-local identity of its
own; each identity variable applies on its own. With the key set, the command writes it to
`~/.ssh/agtk_signing_key` (mode 0600, in a 0700 directory), validates it, configures git to sign
commits and tags with `ssh-keygen` instead of the platform's signer, proves the setup by signing a
throwaway blob, and prints the key's fingerprint, never the key. With the key unset it touches no
signing setting and clears nothing an earlier run set; commits then show as Unverified on GitHub.

With none of the three set the command changes nothing, prints one line saying so, and exits 0.
It exports nothing to the environment and is exempt from the background update check.

The default stack's `SessionStart` hook (`cloud-init-claude-session-start`) runs
`agtk cloud init --render` at every session start, on startup, resume, clear and compact. A
session with none of the three variables set is left alone: the hook prints one line and exits
before it touches `PATH` or runs any `agtk` command. With one set, a failing or missing `agtk`
never blocks the session; a failure is reported in the session's context.

`--render` runs `agtk render` in each checkout directly under `--render-root` (default
`/home/user`) that holds an entry manifest and a lockfile committed at `HEAD`. Only directories
directly under the root are considered. It never runs `agtk sync`, so no ref is resolved over the
network. A render refused because a hand-placed file sits at a render target is reported per
checkout; rerun `agtk render --force` in that checkout by hand.

### Environment recipe

Configure the environment, not the setup script's git commands. The platform overwrites git's
global config at every session start, so a script that runs `git config --global` at build time
does not hold; the hook reapplies the identity each session.

1. Generate a key used only for signing, with no passphrase, and register its public half on
   GitHub as a **signing** key (not an authentication key). Rotate it periodically.
2. Set three environment variables on the environment:
   - `AGTK_GH_USER` and `AGTK_GH_EMAIL`: your identity.
   - `AGTK_SIGNING_KEY_B64`: the private key, base64-encoded on one line. On Linux:
     `base64 -w0 < key`. On macOS: `base64 < key | tr -d '\n'`.
3. List only `stacks/default.yaml` in the consumer repo's manifest. It brings the hook and the
   `no-attribution` stack, which switches off Claude Code's attribution lines.
4. Keep the setup script's install-agtk and `agtk render` lines, and drop its key block (the
   step that wrote `~/.ssh/commit_signing_key`). Under the default policy `.claude/` is
   gitignored, so a fresh checkout has none, and the hook cannot create the directory that holds
   it. The setup script's `agtk render` is the first render; the hook is the per-session one. The
   script may also call `agtk cloud init --render` itself.

Whether the setup script re-runs at session start or only when the environment is built is not
verified; the recipe does not depend on either. Without a key, commits show as Unverified on
GitHub.

**Limit.** The agent runs as the same user as the command, so it can read the key, from the
variable and from the file. The mitigation is blast radius, not secrecy: use a signing-only key,
registered on GitHub only as a signing key, and rotate it. See
[ADR 0027](adr/0027-cloud-init-signs-with-the-users-own-key-when-one-is-supplied.md) for the decision and the rejected alternatives.

## Choosing where to apply from

By default every command reads `./.agentic-toolkit.yaml`, writes the
lockfile next to it, and renders into the current directory. Two global
flags decouple *where the stack lives* from *where output lands* (they are
mutually exclusive):

- **`--config <path>`** — point at an entry manifest elsewhere. The lockfile
  lands next to that file and local `./…` refs resolve from its directory,
  but rendered output still goes to the working directory. This is the
  bare-repo + worktree workflow: run from the bare root, point `--config`
  at a worktree's manifest.

- **`--source <dir>` [`--stack <name>`]** — apply a *toolkit source tree*
  that lives elsewhere on disk (a checkout with `stacks/` and
  `definitions/`), exactly as if `agtk` were run inside it. Bare-name
  definitions resolve against `<dir>/definitions/`, while the lockfile and
  rendered output land in the working directory — the source tree is never
  written to. Without `--stack`, the entry is `<dir>/.agentic-toolkit.yaml`;
  with it, the entry is `<dir>/stacks/<name>.yaml`.

  ```bash
  # render this repo's `rust` stack into the current project,
  # sourcing skills/agents/hooks from a sibling checkout:
  agtk --source ../agentic-toolkit --stack rust sync
  ```

  Use this for local development of a toolkit before pushing, or to apply a
  shared toolkit checkout without going through a git URL.

## Recipes

### Recipe 1 — compose a single shared stack

You only want what a published stack already gives you:

```yaml
stacks:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
```

### Recipe 2 — compose two shared stacks

```yaml
stacks:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/bare-repos.yaml@main
```

Later entries win on conflicts, so layer narrower/opinionated stacks
after broader ones.

### Recipe 3 — add specific definitions from another repo

You like the toolkit's `default` stack, and want to add the `use-gt`
skill and the `worktree-per-session` rule from `pedromvgomes/gt`. An entry
manifest has no per-category lists of its own — `stacks:` only composes
whole stacks — so name the one-off entries in a small local stack instead:

```yaml
# stacks/extras.yaml
skills:
  - github.com/pedromvgomes/gt.git/agentic/skills/use-gt@main
rules:
  - github.com/pedromvgomes/gt.git/rules/worktree-per-session.md@main
```

```yaml
# .agentic-toolkit.yaml
stacks:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
  - ./stacks/extras.yaml
```

No need to publish `stacks/extras.yaml` anywhere else — a local `./path`
stack is enough to carry a handful of definitions you only need in this
repo.

### Recipe 4 — keep your own definitions in this repo

You have project-local skills you don't want to publish anywhere. Drop
them under `<root>/<category>/` (default root `agentic`) and they render
without being listed anywhere:

```
agentic/skills/code-review-style/SKILL.md
agentic/skills/perf-profiler/SKILL.md
agentic/rules/no-experimental-apis.md
```

```yaml
# .agentic-toolkit.yaml
stacks:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
```

Local skills follow the same bundle layout as published ones
(`<dir>/SKILL.md` with companion files alongside). You pick the folder
under `agentic/skills/` — there's no further naming step.

If you'd rather keep your own content somewhere other than `agentic/`, set
`root`:

```yaml
root: ./internal-toolkit
stacks:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
```

and drop content under `./internal-toolkit/skills/`,
`./internal-toolkit/rules/`, and so on instead.

### Recipe 5 — compose several stacks

Once you're pulling in more than one thing, list them all under `stacks:`
— this repo does exactly this for its own definitions:

```yaml
# .agentic-toolkit.yaml
stacks:
  - ./stacks/default.yaml
  - ./stacks/plannotator.yaml
  - ./stacks/serena.yaml
  - ./stacks/rtk.yaml
```

Each entry is a stack file with the shape described in **Stack fields**
above — it can `extends:` further, define its own `root`, and add its own
per-category entries.

### Recipe 6 — suppress attribution

The `no-attribution` stack lists the `no-attribution` setting, which renders
`attribution: {commit: false, pr: false, sessionUrl: false}` into
`.claude/settings.json` so Claude Code adds no `Co-Authored-By` trailer, "Generated with"
line or session link to commits and pull requests, in cloud and local sessions alike.

The default stack extends it, so a consumer on the default stack already has it. To adopt it
without the default stack, list it like any other stack:

```yaml
stacks:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/no-attribution.yaml@main
```

The setting carries `platforms: [claude]`, so the `codex` platform renders nothing for it.

Limits of the stack:

- **Scope precedence.** `attribution` lands in the project-scope `.claude/settings.json`. A
  higher-precedence scope (managed settings, for one) that sets its own `attribution` overrides
  it. Whether a cloud platform sets one is not verified.
- **Rendering is a prerequisite.** A fresh cloud checkout has no rendered `.claude/` until
  something runs `agtk render`, so the stack alone does not cover a cloud session that has not
  rendered. A local checkout that has rendered once is covered.
- **GitHub MCP tools.** Comments and reviews posted through the GitHub MCP tools get a
  "Generated by Claude Code" footer that no documented setting removes. This rests on a single
  issue-tracker report (claude-code issue 62791), not on the settings reference. The
  `no-authoring-footers` PreToolUse hook matches Bash only, so it cannot stop a footer the MCP
  server appends.
- **No validation of settings keys.** The Claude adapter copies settings keys verbatim, so an
  `attribution` of the wrong shape renders without error and has no effect.

## Common pitfalls

- **Wrong filename.** It's `.agentic-toolkit.yaml`, with a leading dot
  and `.yaml` (not `.yml`). The lockfile is
  `.agentic-toolkit.lock.yaml`.
- **Forgetting `.git/`.** External URL entries **must** include `.git/`
  as the boundary between the repo URL and the in-repo path. Without
  it, the resolver can't locate the bundle/file.
- **Bare name in `extends:`/`stacks:`.** Bare names only resolve to
  definitions inside a stack's per-category lists, not to stacks
  themselves. Stack imports must use a URL or `./path`.
- **Old-format config.** If you see a `legacy_config` parse error
  pointing at `source:` / `presets:` / `externals:` — that's the
  v1 schema. See [MIGRATION.md](MIGRATION.md) for the upgrade path.
- **Skipping the lockfile.** `agtk plan`, `agtk render`, and `agtk
  fetch` all refuse to run without `.agentic-toolkit.lock.yaml`. Run
  `agtk lock` first (or use `agtk sync`).
