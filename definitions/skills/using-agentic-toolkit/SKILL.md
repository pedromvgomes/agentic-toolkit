---
name: using-agentic-toolkit
description: >-
  Use when changing what agent tooling this repo ships through agentic-toolkit (`agtk`) — the
  entry manifest `.agentic-toolkit.yaml`, the definitions under its convention root, or the
  stacks it composes. Triggers on phrases like "add an MCP server to this repo's agent tooling",
  "add a local skill/rule/instruction/command/hook/setting", "import a skill from another repo",
  "pull in that agent from the toolkit", "create a local stack", "why isn't my skill rendering",
  or any request to change what lands in `.claude/`, `CLAUDE.md`, `.agents/`, `AGENTS.md` or
  `.codex/`. Covers where each category's files go, which `agtk` command has to run afterwards,
  and the collision rules that decide which definition wins. Do NOT hand-edit rendered output —
  every one of those paths is overwritten by the next render.
---

# Using agentic-toolkit in a consumer repo

`agtk` renders **definitions** — skill, agent, rule, instruction, command, hook, mcp, setting —
into each platform's native layout. A consumer repo gets definitions from two places, and they
are configured differently.

Procedures, directory shapes and gotchas live in [REFERENCE.md](REFERENCE.md). This file is the
mental model you need before opening it.

## The entry manifest is not a stack

`.agentic-toolkit.yaml` at the repo root is the **entry manifest**. A file under `stacks/*.yaml`
— here or in any other repo — is a **stack**. They are different types (ADR 0016), and the
difference is what most mistakes come down to:

| | Entry manifest | Stack |
|---|---|---|
| Composes shared content with | `stacks:` | `extends:` |
| Names its own definitions | never — scanned by convention under `root:` | per-category lists (`skills:`, `mcp:`, …) |
| `root:` / `context:` / `memory:` / `platforms:` | native fields | **do not exist on this type** |

A stack that sets `memory:` or `platforms:` fails to parse, naming the field and pointing at the
entry manifest. Where a repo keeps its memory store and which platforms it renders for are facts
about the consumer, not something a shared stack asserts on its behalf.

## Two ways to get a definition

- **Your own, by convention.** Anything under `<root>/<category>/` (default root `agentic`, so
  `agentic/skills/`, `agentic/mcp/`, …) renders without being listed anywhere. This is the path
  for content this repo owns.
- **Someone else's, through a stack.** `stacks:` on the entry manifest and `extends:` on a stack
  each take a URL (with `.git/` as the boundary between repo and in-repo path, optional `@ref`)
  or a local `./path`. A bare name is never valid there — bare names only resolve inside a
  stack's own per-category lists, against that stack's `root:` (default `definitions`).

The entry manifest has no per-category lists, so importing a handful of individual definitions
from another repo means writing a small local stack and listing it under `stacks:`. REFERENCE.md
has the recipe.

## Which command to run

| What you changed | Run |
|---|---|
| A file under `<root>/<category>/`, or a local `./path` stack | `agtk render` |
| `stacks:`/`extends:` entry pointing at a URL, or a `@ref` | `agtk lock` then `agtk render`, or `agtk sync` |

`lock` is the only command that resolves a ref to a SHA (`git ls-remote`); `sync` runs it when
the lockfile is missing or older than the entry manifest. `render`, `plan` and `fetch` work
strictly from `.agentic-toolkit.lock.yaml` and fail closed with `source not pinned in lockfile`
against a URL and ref the lockfile has no pin for. They may still fetch a *pinned* SHA on a cache
miss — what they never do is decide which commit a ref means.

Locally-scanned content and local `./path` stacks are in the consumer's own filesystem and need
no pin, which is why `render` alone is enough for them.

## Before you write anything

- Read [REFERENCE.md](REFERENCE.md) for the per-category directory shape and the collision rules.
- Check `definitions/SCHEMA.md` in the source repo you are pulling from for the frontmatter each
  category requires — `description` is required everywhere, and hook/mcp/setting have required
  fields of their own.
- Never edit rendered output: `.claude/`, `.agents/`, `.codex/`, the managed region of
  `CLAUDE.md`, and the whole of `AGENTS.md` under the `codex` platform are all rewritten on the
  next render.
