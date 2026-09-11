---
description: What this repository is, how to build and test it, and the boundaries an agent works inside. Scoped to agentic-toolkit itself — no consumer stack should list it.
---

# agentic-toolkit

A toolkit that distributes agent definitions (skills, agents, rules, hooks, MCP servers,
settings) from source repos into consumer repos, and drives a repo-resident memory store and a
multi-model code-review flow. Two things in one repo: a catalog of definitions under
`definitions/` plus shareable `stacks/` bundling them, and the `agtk` CLI that consumer repos run
to render a chosen stack into their platform's native layout (`.claude/`, `CLAUDE.md`,
`.agents/`, `AGENTS.md`, `.codex/`).

Full documentation: [README.md](README.md), [docs/CONSUMER-GUIDE.md](docs/CONSUMER-GUIDE.md).

For the project's domain vocabulary — **Definition**, **Stack**, **Note**, **Reviewer**,
**Panel**, **Handoff**, **Task**, **Platform**, **Adapter**, and more — read [CONTEXT.md](CONTEXT.md)
before writing about any of those concepts. Terms there are precise and words listed under
`_Avoid_` are deliberately banned; using a wrong synonym (e.g. "preset" for **Stack**, "gate" for
**Predecessor**) reads as a misunderstanding, not a style choice.

## Commands

- **Build**: `make build` (writes `./bin/agtk`, stamped with `git describe`)
- **Install**: `make install`
- **Test**: `make test` (`go test ./...`)
- **Format**: `make fmt` (`gofmt -s -w .`)
- **Vet**: `make vet`
- **Full check** (fmt + vet + test + gofmt cleanliness): `make check`

Requires Go 1.26+.

## Project structure

- `source/toolkit/` — the Go tooling, all of it
- `source/toolkit/cmd/agtk/` — CLI entrypoint
- `source/toolkit/internal/` — CLI implementation (private packages): `resolver`, `lockfile`,
  `stack`, `definitions`, `sourceref`/`sourcestore` (fetch/cache), `review`/`reviewrun`/`reviewpost`/
  `reviewapprove` (code-review flow), `curator`, `memory`, `githubapp`, `updater`/`updatecheck`,
  `adapters` (per-platform render targets, over the shared `adapters/fsops`)
- `definitions/` — the catalog, one directory per category: `agents/`, `commands/`, `hooks/`,
  `instructions/`, `mcp/`, `settings/`, `skills/`. `rules/` is a valid category the schema
  defines and this repo currently ships none of. See
  [`definitions/SCHEMA.md`](definitions/SCHEMA.md) for the shape each category's files must take.
- `stacks/` — shareable manifests consumer repos `extends:`: `default.yaml` (the feature flow,
  workflow-agnostic skills, memory-first and plan-approval instructions), plus one stack per
  integration (`serena.yaml`, `rtk.yaml`, `plannotator.yaml`)
- `docs/adr/` — architecture decision records; consult before changing something an ADR already
  settled
- `docs/FEATURE-FLOW.md` — the two-stage feature flow (`/plan-feature` → handoff → implement →
  review → open PR)
- `source/toolkit/internal/cli/tests/` — stack-render tests (e.g. `default_stack_render_test.go`,
  `feature_flow_stack_render_test.go`) that assert what `agtk render` writes for a given stack

## Definitions catalog

Adding or changing a definition means editing a file under `definitions/<category>/...` and, if
it should ship by default, wiring it into `stacks/default.yaml` (or another stack). Read
[`definitions/SCHEMA.md`](definitions/SCHEMA.md) first — each category has a fixed shape, and
`source/toolkit/internal/definitions` validates against it at render time. A definition no
stack lists is never opened and never reported: adding the file is not adding the definition.

## Boundaries

### Always do
- Run `make check` before considering a change to Go code done
- Look up unfamiliar domain terms in [CONTEXT.md](CONTEXT.md) rather than guessing a synonym
- Check `docs/adr/` before revisiting a decision that already has one
- Add a stack-render test under `source/toolkit/internal/cli/tests/` when a stack's rendered output changes

### Ask first
- Renumbering or superseding an existing ADR
- Changing what a released stack (`stacks/*.yaml`) bundles by default for existing consumers

### Never do
- Duplicate CONTEXT.md's glossary definitions elsewhere — link to it instead
- Commit secrets, API keys, or GitHub App credentials
- Hand-edit `AGENTS.md`, `CLAUDE.md`'s managed region, or anything under `.claude/`, `.agents/`
  and `.codex/` — every one of them is rendered from this catalog, and an edit there is
  overwritten by the next `agtk render`
