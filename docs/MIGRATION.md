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

- `platforms:` is no longer a config field. Platform targeting is
  applied at render time per command (future work — currently agtk
  renders for all platforms each definition supports).
- The `agtk init --source` flag is now `agtk init --extends`.
- A new `agtk sync` command runs `lock` (if stale) + `fetch` + `render`
  in one step, suitable for the everyday `pull-and-update` workflow.
