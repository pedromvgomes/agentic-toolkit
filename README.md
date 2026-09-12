# agentic-toolkit

A toolkit for initializing repositories with shared agent assets — skills, agents, rules, instructions, commands, hooks, MCP configs — across multiple agentic platforms. Claude Code and Codex render today; a stack opts into the ones it wants with `platforms:`.

This repository is two things in one:

- **A catalog of definitions** under `definitions/` plus a set of shareable **stacks** under `stacks/` that bundle them — the reusable assets that consumer repos extend.
- **A CLI (`agtk`)** that consumer repos run to render the chosen stacks into their target platform's native layout.

A consumer repo opts in by committing a single **`.agentic-toolkit.yaml`** at its root: an entry-point stack manifest that extends one or more shareable stacks (URLs to other repos' `stacks/*.yaml`) and optionally adds project-local definitions on top. See [docs/CONSUMER-GUIDE.md](docs/CONSUMER-GUIDE.md) for the full walkthrough.

## Install

The installer downloads the latest release archive for your platform, verifies its sha256 against the release's `checksums.txt`, and drops `agtk` on `$PATH`. No Go toolchain required.

```bash
curl -fsSL https://raw.githubusercontent.com/pedromvgomes/agentic-toolkit/main/install.sh | sh
```

The script also installs shell completion for `bash`, `zsh`, or `fish` based on `$SHELL` to a path the shell already loads from when possible: brew's `share/zsh/site-functions/` for zsh-on-brew users, otherwise `~/.zsh/completions/` (with an `fpath` hint), `~/.local/share/bash-completion/completions/` for bash, `~/.config/fish/completions/` for fish. Opt out with `AGTK_NO_COMPLETION=1`.

Environment overrides:

- `AGTK_VERSION=v0.1.0` — pin a specific tag instead of the latest release.
- `AGTK_INSTALL_DIR=$HOME/bin` — pick the install dir. Default: `/usr/local/bin` if writable, else `~/.local/bin`.
- `AGTK_NO_COMPLETION=1` — skip the shell-completion install.
- `AGTK_OS` / `AGTK_ARCH` — override platform detection (rarely needed).

After install, `agtk update` upgrades in place from the same release archives — no `curl | sh` needed for follow-ups.

These two are the whole of it: the installer and `agtk update`. `go install` is not a supported
path and cannot work — `go.mod` lives at `source/toolkit/go.mod` while declaring the module path
`github.com/pedromvgomes/agentic-toolkit`, and the Go toolchain fetches a module in a
subdirectory only when the declared path is the repo root plus that subdirectory. Keeping the
declared path short is what stops every import repeating `source/toolkit`, and `agtk` is a
binary rather than a library, so nothing is given up: every package here is `internal/`, and
release tags stay plain semver instead of needing a `source/toolkit/` prefix.

## What you get

- **Definition catalog** under `definitions/` covering eight typed categories: `skill`, `agent`, `command`, `rule`, `instruction`, `hook`, `mcp`, `setting`. See [`definitions/SCHEMA.md`](definitions/SCHEMA.md) for shapes.
- **Shareable stacks** under `stacks/` that bundle catalog definitions for consumers to extend: `default.yaml` (the feature flow, the workflow-agnostic skills, the memory-first and plan-approval instructions), and one stack per integration — `serena.yaml`, `rtk.yaml`, `plannotator.yaml`.
- **`agtk` CLI** with `init`, `lock`, `fetch`, `plan`, `render`, `sync`, `status`, `memory`, `update`. Run any subcommand with `--help` for flags.
- **Lockfile-driven workflow.** `agtk lock` resolves the entry-point stack's `extends:` graph to commit SHAs; `agtk fetch` hydrates the cache deterministically; `agtk render` writes Claude Code's expected layout under `.claude/`. `agtk sync` collapses all three into one command for the common case.
- **Two-stage feature flow.** `/plan-feature` plans on opus without reading the codebase, has its draft reviewed by a second model, and writes a handoff; you run `/clear`; a session-start hook points the fresh sonnet session at the work, which lands one task per subagent, reviews in a capped loop and opens the PR. In `stacks/default.yaml`, which also sets the session's default model to sonnet. See [docs/FEATURE-FLOW.md](docs/FEATURE-FLOW.md).
- **Repo-resident memory.** `agtk memory` manages a committed store of durable notes about the consumer's own codebase — invariants, rationale, gotchas, dead ends — each anchored to the content it was derived from, so a note that has gone stale says so. Deterministic and model-free, hence safe in hooks and CI. See the memory section of [docs/CONSUMER-GUIDE.md](docs/CONSUMER-GUIDE.md).
- **Auto-update** that checks GitHub releases in the background and self-replaces from the verified archive when you run `agtk update`.

## Repository layout

```
agentic-toolkit/
  source/toolkit/       # the Go tooling, and the Go module
    go.mod, go.sum      # the module root — every `go` command runs here
    cmd/agtk/           # CLI entrypoint
    internal/           # CLI implementation (private packages)
    tools/              # code generators run via `go generate`
  definitions/          # the catalog
    rules/              # rule definitions
    skills/             # skill definitions
    instructions/       # CLAUDE.md / AGENTS.md sections
    ...
  stacks/               # shareable stack manifests for consumers to extend
    default.yaml
    serena.yaml
    ...
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
