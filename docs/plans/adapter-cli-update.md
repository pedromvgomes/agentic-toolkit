# Plan — generic settings, Claude adapter, CLI ergonomics, auto-update

Status: Slice A0 pending kickoff. A/B/C unstarted.
Created: 2026-04-30.

Four sequential PRs that turn the current Plan-producing pipeline into a renderable, observable, self-updating CLI. Order is **A0 → A → B → C**; each builds on the previous, do not skip ahead.

---

## Conventions (apply to all slices)

- Branch off `main`, worktree at `feature/<slice-name>/` per `definitions/rules/bare-repos.md`.
- One commit per slice, one PR per slice. Personal account credential helper:
  `GH_TOKEN="$(gh auth token --user pedromvgomes)" gh pr create --repo pedromvgomes/agentic-toolkit ...`
- Tests live in `internal/<pkg>/tests/`, exercise public API only.
- `gofmt -s -l .` must report zero files; CI enforces.
- Surface design questions inline before locking new contracts (plan-approval rule).
- Minimal-schemas: every field needs concrete justification; YAGNI flag = surface as a question.

---

## Slice A0 — Generic `setting` category

Goal: let definitions declare arbitrary `settings.json` fragments so a stack can manage permissions, env, model, sandbox, etc. — not just hooks and MCPs. Schema-only PR; render-time application happens in Slice A.

### Schema change
- New category `setting` (added to `Category` enum in `internal/definitions/types.go`).
- File-shaped (single file per definition, no bundle). Naming via existing `validateNameForCategory` — same rules as hook/mcp.
- Definition file shape:
  ```
  ---
  name: deny-dangerous-shell
  description: Block destructive shell commands
  category: setting
  ---
  permissions:
    deny:
      - "Bash(rm -rf:*)"
      - "Bash(sudo rm:*)"
  ```
  - Frontmatter: `name` + `description` (matches other categories).
  - Body: YAML fragment that maps 1:1 to `settings.json` structure. Top-level keys in the fragment are top-level keys in `settings.json`.
  - Parsed body retained as `map[string]any` (or equivalent) for downstream merge.

### Render-time semantics (specified here, implemented in Slice A)
- **Top-level key ownership.** agtk owns whole top-level keys touched by any `setting` definition. Tracked in `_meta.agtk.managed: ["permissions", "env", "hooks", "mcpServers", ...]`. Users can hand-edit any other top-level key freely.
- **Conflict policy.** Last-preset-wins at the top-level key. If two `setting` definitions touch the same top-level key, the later preset wins the *whole key* (no leaf-merge across definitions). Override losers surface via existing `DiagOverride` diagnostic.
- **Hook + MCP coexistence.** `hook` and `mcp` remain first-class categories with their own validated shapes. They render to `settings.json.hooks` and `settings.json.mcpServers` respectively, and contribute to the same `_meta.agtk.managed` list.

### External preset-ref grammar
- `settings::<repo-url>.git/<in-repo-path>@<ref>` — file-shaped, mirrors rule/hook/mcp grammar. URL ends at the file itself (extension included).

### Files touched
- `internal/definitions/types.go` — add `CategorySetting`, plural form, body field on `Definition`.
- `internal/definitions/parser.go` — parse YAML fragment from body for `setting` files; thread through `ParseInCatalog`, `ParseFile`.
- `internal/definitions/walk.go` — walk `definitions/settings/` directory.
- `internal/definitions/preset.go` — accept `settings::` ref prefix.
- `internal/resolver/resolver.go` — already category-agnostic for file refs; verify `setting` flows through.
- `tools/schemagen/main.go` — emit `setting` block in `SCHEMA.md`.
- `definitions/SCHEMA.md`, `definitions/CONFIG-SCHEMA.md` — regenerated.
- `internal/definitions/tests/` and `internal/resolver/tests/` — coverage for parse, walk, dedupe, external file ref.

### Locked design decisions
- **File extension `.yaml`** — pure YAML manifest, mirroring `hook` and `mcp` precedent. No markdown body.
- **Path:** `definitions/settings/<name>.yaml`. Plural dir name: `settings`.
- **Struct:**
  ```go
  type Setting struct {
      Common `yaml:",inline"`
      Value  map[string]any `yaml:"value"`
  }
  ```
  Top-level: `name`, `description`, plus `value:` containing the settings fragment. The `value:` indirection separates metadata from payload and avoids future name collisions if Claude Code's settings.json schema grows a `name` or `description` key.
- **Body parsing:** pass-through — `value` decodes into `map[string]any`. agtk does not validate Claude Code's settings.json key vocabulary.
- **Validation in `validate()`:** `value` must be non-nil and non-empty (a Setting with no payload is meaningless). `value` keys must be strings (YAML default).
- **External preset-ref grammar:** `settings::<repo-url>.git/<in-repo-path>.yaml@<ref>` — file-shaped, mirrors hook/mcp grammar.

### Out of scope (deferred to slice A)
- Actual leaf-merge into `settings.json` and marker writing.
- AGENTS.md handling.
- Bundle FS exposure on `PlannedDefinition`.

---

## Slice A — Claude adapter + `agtk render`

Goal: render a resolved Plan to disk under `~/.claude/` (user) or `./.claude/` (project). Depends on Slice A0 landing.

### Package layout
- `internal/adapters/claude/` — `Render(plan *resolver.Plan, opts Options) error`
- `internal/cli/render.go` — `agtk render [--scope user|project] [--dry-run]`
- `internal/adapters/claude/tests/` — package tests with programmatic plan fixtures

### Surface
```go
type Scope int
const (
    ScopeUser Scope = iota  // ~/.claude/
    ScopeProject            // ./.claude/
)

type Options struct {
    Scope  Scope
    Root   string  // override for tests; empty = derive from Scope
    DryRun bool
}
```

### Render map

`<scope-root>` = `./.claude/` (project scope) or `~/.claude/` (user scope).
`<project-root>` = `./` (project scope) or `~/` (user scope, but no AGENTS.md fallback).

| Category | Render target | Notes |
|---|---|---|
| `skill` | `<scope-root>/skills/<name>/SKILL.md` (+ bundle files) | Whole-file ownership; bundle copy from `SourceFS` |
| `agent` | `<scope-root>/agents/<name>/AGENT.md` (+ bundle files) | Whole-file ownership; bundle copy from `SourceFS` |
| `command` | `<scope-root>/commands/<name>.md` | Whole-file ownership; nested name `git/commit` → `commands/git/commit.md` |
| `rule` | `<scope-root>/rules/<name>.md` | First-class Claude Code mechanism (auto-loaded). Optional `paths:` frontmatter for path-scoping (deferred until rule schema gains a paths field) |
| `instruction` | Managed block in `<project-root>/CLAUDE.md` | See CLAUDE.md generation flow below |
| `hook` | `<scope-root>/settings.json` `hooks` key (top-level overwrite) | Marker tracked in `_meta.agtk.managed` |
| `mcp` | `<scope-root>/settings.json` `mcpServers` key (top-level overwrite) | Marker tracked in `_meta.agtk.managed` |
| `setting` | `<scope-root>/settings.json` arbitrary top-level keys | Defined in Slice A0; top-level overwrite per definition; marker tracked in `_meta.agtk.managed` |

### CLAUDE.md generation flow (project scope)
1. If `<project-root>/CLAUDE.md` exists → manage region inside it (markers below).
2. Else if `<project-root>/AGENTS.md` exists → seed `<project-root>/CLAUDE.md` with:
   ```
   @AGENTS.md

   <!-- BEGIN AGTK MANAGED -->
   ...instructions...
   <!-- END AGTK MANAGED -->
   ```
3. Else → create `<project-root>/CLAUDE.md` containing only the managed block.

User scope: write directly to `~/.claude/CLAUDE.md`; no AGENTS.md fallback.

### Managed-region markers
- **CLAUDE.md** (mixed-ownership, supports HTML comments):
  ```
  <!-- BEGIN AGTK MANAGED -->
  <!-- END AGTK MANAGED -->
  ```
  Content outside the markers is preserved verbatim. Block-level HTML comments are stripped before injection into Claude Code's context (per Claude Code memory docs), so the markers cost zero context tokens.
- **settings.json** (mixed-ownership, no comments): top-level key `_meta.agtk.managed: ["<keys>"]`. Renderer overwrites only those keys; preserves user keys. Per `project_settings_merge_semantics.md` memory entry.
- **Whole-owned files** (skills/agents/commands/rules): no markers; idempotent (write only if content differs); collision policy below.

### Existing-file collision policy (whole-owned files)
- Refuse and require `--force` when a target file exists but agtk did not create it (no recognizable agtk content / not in lockfile-tracked render manifest).
- Render manifest: optional sidecar `<scope-root>/.agtk-manifest.json` recording last-rendered file paths + their content hashes. Files in the manifest can be overwritten; files not in the manifest trigger the refuse-without-force path.

### Bundle companion files — resolver gap
`PlannedDefinition` does not currently expose the source filesystem. Slice A adds:
```go
type PlannedDefinition struct {
    // ... existing fields ...
    SourceFS fs.FS  // rooted at the entry's source dir; renderer walks siblings of EntryPath for bundle copy
}
```
For bundles (skill/agent), root = bundle dir; renderer copies every file under it except the entry file (already in `Definition`). For file categories, no companion copy.

### Tests
- Programmatic plan fixtures in `testdata/`.
- Cases: dry-run produces no writes; idempotent re-render is a no-op; settings merge preserves untracked user keys; bundle copy includes resources/; managed-region preservation in CLAUDE.md; project vs user scope.

### Open questions (resolve at implementation time)
- `paths:` frontmatter for `rule` definitions (path-scoped rule loading per Claude Code docs) — pass through to rendered file if the source definition has it. The `rule` schema doesn't currently have a `paths` field; adding it is a separate small schema change, deferrable.
- Manifest format: JSON for now; revisit if it grows.

---

## Slice B — CLI ergonomics

Goal: observability + CI guardrails on the existing pipeline. Depends on Slice A landing (status compares rendered state).

### Commands
- `agtk status` — read config + lockfile + rendered state, report drift in three buckets:
  - config-vs-lockfile (sources changed since last lock)
  - lockfile-vs-cache (pinned SHA missing locally)
  - rendered-state (Plan vs on-disk in target scope)
- `agtk lock --frozen` — re-resolve, fail if lockfile would change. CI guard.
- `--json` flag on `plan`, `status`, `lock` — machine-readable output. Schema versioned via `"version": 1`.

### Diagnostics improvements
- Source classification (primary/declared/implicit) surfaced in `plan` output.
- Override diagnostics shown by default, suppressible with `--quiet`.

### Tests
- Each command tested via `Env`-injected stdin/stdout/stderr; no real network.
- Fixture configs with deliberate drift conditions.

---

## Slice C — Auto-update

Goal: detect newer releases, surface non-intrusively, install on demand.

### Decisions locked
- **Version source:** `internal/version.Version` (already exists, default `"dev"`), set via `-ldflags "-X github.com/pedromvgomes/agentic-toolkit/internal/version.Version=vX.Y.Z"` at release-build time. `runtime/debug.ReadBuildInfo()` fallback for `go install` users. `"dev"` skips checks entirely.
- **Tag-triggered release:** pushing tag `vX.Y.Z` triggers GitHub Actions running goreleaser → publishes archives + release notes for `vX.Y.Z`.
- **Release notes flow:** goreleaser uses `docs/releases/vX.Y.Z.md` if present (else `--generate-notes`). Footer template:
  ```yaml
  release:
    footer: |
      **Full Changelog**: https://github.com/{{ .Env.GITHUB_REPOSITORY }}/compare/{{ .PreviousTag }}...{{ .Tag }}
  ```
- **User config** `${XDG_CONFIG_HOME:-~/.config}/agentic-toolkit/config.yaml`:
  ```yaml
  auto_update:
    enabled: true        # default
    check_interval: 24h  # parsed as time.Duration
  ```
  New package: `internal/userconfig`.
- **State** `${XDG_STATE_HOME:-~/.local/state}/agentic-toolkit/state.yaml`:
  ```yaml
  last_update_check: 2026-04-30T10:15:00Z
  latest_known_version: v1.4.0
  ```
- **Goroutine plumbing:**
  - Spawn in cobra `PersistentPreRunE`, pass `<-chan UpdateInfo` into `Env`.
  - 2s HTTP timeout against `GET https://api.github.com/repos/pedromvgomes/agentic-toolkit/releases/latest`.
  - After main command returns success, non-blocking select; print one-liner if newer arrived in time; else drop.
  - Gates (all must hold): `term.IsTerminal(stdout)`, `Version != "dev"`, `auto_update.enabled`, `now - last_check >= check_interval`.
  - One-liner format: `agtk vX.Y.Z is available (you're on vA.B.C). Run 'agtk update' to install.`
- **Update command:**
  - `agtk update` — check, prompt, install if confirmed.
  - `agtk update --check` — exit 0 if up-to-date, exit 10 if newer available (scriptable). Print latest version to stdout.
  - `agtk update --yes` — skip prompt, install if newer.
- **API auth:** unauth (60 req/hr per IP) is fine for personal use. Defer auth until rate-limited.
- **Tests:** `LatestVersionProvider` interface, stubbed in tests. Network never hit.

### Install mechanism — locked
`agtk update` performs **self-replace from the goreleaser archive**. Steps:
1. Resolve current platform (`runtime.GOOS`/`GOARCH`).
2. Download matching archive from the latest GitHub release (`agtk_<version>_<os>_<arch>.tar.gz` — name format pinned in `.goreleaser.yaml`).
3. Verify checksum against the release's `checksums.txt` (also published by goreleaser).
4. Atomic-rename the new binary over the running executable (Unix-safe).
5. Print success + new version.

Implementation: `github.com/minio/selfupdate` (or `github.com/inconshreveable/go-update`) — both handle the atomic-rename + Windows quirks. Pick at implementation time. Estimated ~150 LOC including platform detection + checksum verify.

Failure modes to handle explicitly:
- Network failure / GitHub down → clear error, exit non-zero.
- Checksum mismatch → abort, do not replace, surface verbatim.
- No matching archive for platform → error listing available platforms.
- Permission denied on rename (binary in `/usr/local/bin` without sudo) → error suggesting `sudo agtk update` or reinstall path.

### Files to add
- `.github/workflows/release.yml` — goreleaser on tag push.
- `.goreleaser.yaml` — build matrix (darwin/linux × arm64/amd64), archive + checksum config, release notes template.
- `internal/userconfig/{loader,types}.go`
- `internal/updatecheck/{provider,throttle,checker}.go`
- `internal/cli/update.go`
- Hook into `internal/cli/root.go` for `PersistentPreRunE` / `PersistentPostRunE`.
- `Makefile` or `script/build` — embeds version via ldflags from `git describe --tags`.

---

## Resume checklist (for future sessions)

When picking up this plan in a new session:

1. Confirm which slice is next (A0 → A → B → C) by checking merged PRs and remaining unchecked items above.
2. Sweep stale worktrees per `definitions/rules/bare-repos.md` before branching.
3. Pull `main` to get latest before creating the new worktree.
4. Slice A depends on Slice A0 having merged (the `setting` category schema must exist before the renderer references it).
5. Slice B depends on Slice A (status compares rendered-state).
6. Slice C is independent of A/B but should land last (auto-update is least urgent).
