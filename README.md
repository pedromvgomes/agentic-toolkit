# agentic-toolkit

A toolkit for initializing repositories with shared agent assets — skills, agents, rules, instructions, commands, hooks, MCP configs — across multiple agentic platforms (Claude Code, Cursor, GitHub Copilot, OpenCode, ...).

This repository is two things in one:

- **A catalog of definitions** under `definitions/` — the reusable assets (skills, rules, etc.) that consumer repos pull from.
- **A CLI (`agtk`)** that consumer repos run to render the chosen definitions into their target platform's native layout.

## Status

Early scaffolding. Nothing is wired end-to-end yet.

## Repository layout

```
agentic-toolkit/
  cmd/agtk/             # CLI entrypoint
  internal/             # CLI implementation (private packages)
  definitions/          # the catalog
    rules/              # rule definitions
    skills/             # skill definitions
    instructions/       # CLAUDE.md / AGENTS.md sections
    ...
```

## Building locally

Requires Go 1.26+.

```bash
go build -o agtk ./cmd/agtk
./agtk --version    # prints "dev" until a release is built
```

## Releases

Released versions are git tags in semver form: `v0.1.0`, `v1.2.3`. Consumer repos pin to a tag, branch, or commit SHA — same convention GitHub Actions uses (`source: github.com/pedromvgomes/agentic-toolkit@v0.1.0`).
