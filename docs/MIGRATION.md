# Migrating from v1 to v2 (stack model)

The v2 schema collapses the previous "consumer config" + "preset" pair
into a single concept — the **stack manifest**. This doc covers the
mechanical translation from v1.

If you have an existing `.agentic-toolkit.yaml` that uses `source:`,
`externals:`, or `presets:`, you'll see a parser error like:

```
.agentic-toolkit.yaml: legacy_config: "source" is a v1 schema field;
this is a stack manifest (v2). See docs/MIGRATION.md to upgrade.
```

## What changed

| v1 (consumer config + preset) | v2 (stack) |
|-------------------------------|------------|
| `.agentic-toolkit.yaml` with `source:` + `externals:` + `presets:` | `.agentic-toolkit.yaml` is itself a stack manifest with `extends:` and per-category lists |
| Presets live at `definitions/presets/*.yaml` in the primary source | Stacks live at `stacks/*.yaml` at any repo root |
| `presets:` resolves names against the primary source only | `extends:` accepts URLs from any repo |
| Definitions can only be referenced through a preset | Definitions can be listed directly in the consumer file |
| Definitions must live under `definitions/<plural>/<name>...` | Default `root` is `definitions`; override per stack file or use `./path` entries to live anywhere |
| Lockfile schema version: 1 | Lockfile schema version: 2 |

## Translation

### Single-preset consumer

**v1:**
```yaml
source: github.com/pedromvgomes/agentic-toolkit@main
presets:
  - default
```

**v2:**
```yaml
extends:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
```

Note the `.git/` boundary in the URL — it is required for v2.

### Stacked presets

**v1:**
```yaml
source: github.com/pedromvgomes/agentic-toolkit@main
presets:
  - default
  - bare-repos
```

**v2:**
```yaml
extends:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/bare-repos.yaml@main
```

### Externals + custom preset

**v1:**
```yaml
source: github.com/pedromvgomes/agentic-toolkit@main
externals:
  - github.com/anthropics/skills@main
presets:
  - default
  - my-extras   # a preset you committed to agentic-toolkit
```

**v2** — if `my-extras` was just a thin bundle of external definitions,
you no longer need a preset at all. Inline the entries directly:

```yaml
extends:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main

skills:
  - github.com/anthropics/skills.git/skills/skill-creator@main

# (or whatever my-extras contained)
```

The `externals:` field is gone — every external is identified by its
full URL at the point of use.

## Lockfile

Regenerate after migrating the config:

```bash
rm .agentic-toolkit.lock.yaml
agtk lock
```

Or, equivalently, run `agtk sync` and let it lock + fetch + render in
one go.

## Layout-free local definitions

In v1 your local definitions had to live under
`definitions/<plural>/<name>...`. v2 lifts that requirement: use a
`./path` entry to point at any folder in your repo, or set `root:` to
move the convention root somewhere else.

```yaml
# v2 — definitions in any folder
skills:
  - ./team-skills/code-style
  - ./misc/perf-helpers/profiler
```

```yaml
# v2 — convention root moved
root: ./agentic
skills:
  - foo   # → ./agentic/skills/foo/SKILL.md
```

## Other notes

- `platforms:` is back as a v2 field, with different semantics: a
  top-level list of rendering targets (e.g. `codex`), each rendered by
  its own adapter from the same definitions. Omit it and only Claude
  Code renders, same as every stack today. This is distinct from a
  single definition's own `platforms:` field, which narrows which
  targets *that definition* applies to, not which targets the stack
  renders for.
- The `agtk init --source` flag is now `agtk init --stacks`.
- A new `agtk sync` command runs `lock` (if stale) + `fetch` + `render`
  in one step, suitable for the everyday `pull-and-update` workflow.

## Migrating to the entry-manifest split

`.agentic-toolkit.yaml` is no longer a stack manifest — it is an **entry
manifest**, a distinct type with its own shape (see [ADR
0016](adr/0016-the-entry-manifest-is-its-own-type-not-a-stack.md)). This
does not bump the lockfile schema version or change `.agentic-toolkit.lock.yaml`;
it only changes what the entry-point file itself may contain. A `stacks/*.yaml`
file you publish for others to import is unaffected — that's still a stack, and
still uses `extends:` and per-category lists exactly as described above.

If your `.agentic-toolkit.yaml` uses `extends:`, rename it to `stacks:`:

**Before:**
```yaml
extends:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
```

**After:**
```yaml
stacks:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
```

If your `.agentic-toolkit.yaml` also had per-category lists (`skills:`,
`instructions:`, `rules:`, etc.) naming your own definitions directly, move
that content to files under `<root>/<category>/` instead — the entry
manifest no longer accepts per-category lists, so a bare name or path can no
longer be listed there:

**Before:**
```yaml
extends:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
skills:
  - ./internal-skills/code-review-style
rules:
  - ./internal-rules/no-experimental-apis.md
```

**After:**
```yaml
stacks:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
```

with the definitions themselves moved to:

```
agentic/skills/code-review-style/SKILL.md
agentic/rules/no-experimental-apis.md
```

`root` (default `"agentic"`) is where this convention scanning looks; set it
explicitly if your definitions already live under a different folder rather
than moving them:

```yaml
root: ./internal-toolkit
stacks:
  - github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@main
```

If you'd rather keep a handful of externally-sourced definitions that don't
belong to a published stack, name them in a small local stack and compose it
under `stacks:` — see [Recipe 3 in
CONSUMER-GUIDE.md](CONSUMER-GUIDE.md#recipe-3--add-specific-definitions-from-another-repo).

In the entry manifest, `memory:` and `platforms:` need no change — they keep
the same shape. Both are repo properties — where the memory store lives,
which platforms to render — and are accepted only in `.agentic-toolkit.yaml`.
A `stacks/*.yaml` file setting either is refused; move the field to your
entry manifest instead.

`agtk init` scaffolds the new shape (`stacks:`, no per-category lists, a
comment pointing at convention scanning) — re-run it against a fresh file if
you want a template to copy fields from, rather than editing your existing
one from memory.
