# agentic-toolkit

A toolkit for initializing repositories with shared agent assets — skills, agents, rules, instructions, commands, hooks, MCP configs — across multiple agentic platforms (Claude Code, Cursor, GitHub Copilot, OpenCode, ...).

This repository is two things in one:

- **A catalog of definitions** under `definitions/` plus a set of shareable **stacks** under `stacks/` that bundle them — the reusable assets that consumer repos extend.
- **A CLI (`agtk`)** that consumer repos run to render the chosen stacks into their target platform's native layout.

A consumer repo opts in by committing a single **`.agentic-toolkit.yaml`** at its root: an entry-point stack manifest that extends one or more shareable stacks (URLs to other repos' `stacks/*.yaml`) and optionally adds project-local definitions on top. See [docs/CONSUMER-GUIDE.md](docs/CONSUMER-GUIDE.md) for the full walkthrough.

## Install

The installer downloads the latest release archive for your platform, verifies its sha256 against the release's `checksums.txt`, and drops `agtk` on `$PATH`. No Go toolchain required.

```bash
curl -fsSL https://raw.githubusercontent.com/pedromvgomes/agentic-toolkit/main/install.sh | sh
```

The script also installs shell completion for `bash`, `zsh`, or `fish` based on `$SHELL` (no-op for other shells; opt out with `AGTK_NO_COMPLETION=1`).

Environment overrides:

- `AGTK_VERSION=v0.1.0` — pin a specific tag instead of the latest release.
- `AGTK_INSTALL_DIR=$HOME/bin` — pick the install dir. Default: `/usr/local/bin` if writable, else `~/.local/bin`.
- `AGTK_NO_COMPLETION=1` — skip the shell-completion install.
- `AGTK_OS` / `AGTK_ARCH` — override platform detection (rarely needed).

After install, `agtk update` upgrades in place from the same release archives — no `curl | sh` needed for follow-ups.

If you have a Go toolchain and prefer it:

```bash
go install github.com/pedromvgomes/agentic-toolkit/cmd/agtk@latest
```

## What you get

- **Definition catalog** under `definitions/` covering eight typed categories: `skill`, `agent`, `command`, `rule`, `instruction`, `hook`, `mcp`, `setting`. See [`definitions/SCHEMA.md`](definitions/SCHEMA.md) for shapes.
- **Shareable stacks** under `stacks/` that bundle catalog definitions for consumers to extend. Today: `default.yaml` (workflow-agnostic skills + plan-approval) and `bare-repos.yaml` (worktree workflow rule).
- **`agtk` CLI** with `init`, `lock`, `fetch`, `plan`, `render`, `sync`, `status`, `update`. Run any subcommand with `--help` for flags.
- **Lockfile-driven workflow.** `agtk lock` resolves the entry-point stack's `extends:` graph to commit SHAs; `agtk fetch` hydrates the cache deterministically; `agtk render` writes Claude Code's expected layout under `.claude/`. `agtk sync` collapses all three into one command for the common case.
- **Auto-update** that checks GitHub releases in the background and self-replaces from the verified archive when you run `agtk update`.

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
  stacks/               # shareable stack manifests for consumers to extend
    default.yaml
    bare-repos.yaml
  install.sh            # platform-detecting installer (curl | sh entry point)
  .goreleaser.yaml      # release-time build matrix (darwin/linux × amd64/arm64)
```

## Building from source

Requires Go 1.26+. Use the Makefile so the binary is stamped with `git describe`:

```bash
make build      # writes ./bin/agtk
make install    # go install with the same ldflags
```

## Releases

Released versions are git tags in semver form: `v0.1.0`, `v1.2.3`. Pushing a tag triggers `.github/workflows/release.yml`, which runs goreleaser and publishes archives + `checksums.txt` to a GitHub Release. Hand-written notes at `docs/releases/v<X.Y.Z>.md` are picked up automatically; otherwise goreleaser auto-generates from the commit history.

Consumer repos pin to a tag, branch, or commit SHA — same convention GitHub Actions uses (e.g. `extends: github.com/pedromvgomes/agentic-toolkit.git/stacks/default.yaml@v0.1.0`).
