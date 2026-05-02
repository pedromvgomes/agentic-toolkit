# agentic-toolkit

A toolkit for initializing repositories with shared agent assets — skills, agents, rules, instructions, commands, hooks, MCP configs — across multiple agentic platforms (Claude Code, Cursor, GitHub Copilot, OpenCode, ...).

This repository is two things in one:

- **A catalog of definitions** under `definitions/` — the reusable assets (skills, rules, etc.) that consumer repos pull from.
- **A CLI (`agtk`)** that consumer repos run to render the chosen definitions into their target platform's native layout.

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

If you have a Go toolchain and prefer it:

```bash
go install github.com/pedromvgomes/agentic-toolkit/cmd/agtk@latest
```

## What you get

- **Definition catalog** under `definitions/` covering eight typed categories: `skill`, `agent`, `command`, `rule`, `instruction`, `hook`, `mcp`, `setting`. See `definitions/SCHEMA.md` for shapes.
- **`agtk` CLI** with `init`, `lock`, `fetch`, `plan`, `render`, `status`, `update`. Run any subcommand with `--help` for flags.
- **Lockfile-driven workflow.** `agtk lock` resolves preset entries to commit SHAs; `agtk fetch` hydrates the cache deterministically; `agtk render` writes Claude Code's expected layout under `.claude/`.
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

Consumer repos pin to a tag, branch, or commit SHA — same convention GitHub Actions uses (`source: github.com/pedromvgomes/agentic-toolkit@v0.1.0`).
