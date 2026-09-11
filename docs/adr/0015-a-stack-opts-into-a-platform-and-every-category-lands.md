# A stack opts into a platform, and every category lands somewhere

`platforms:` on a **Stack** manifest names the **Platform**s `agtk render` writes for. Omit
it and the render is Claude-only, which is what every repo already got. List `codex` and the
same **Definition**s are written a second time, into Codex's own layout, by a second
**Adapter** over shared machinery.

Reviews have run both Claude Code and Codex per-**Reviewer** for a while, but that subsystem
invokes a model and never touches `internal/stack` or `internal/adapters`. Rendering is the
other direction: files written for a tool to read. A repo that already runs both providers
had no way to say so to the thing that writes its files.

## Considered options

**A `--platform` flag on `agtk render`.** Rejected. Which platforms a repo targets is a
property of the repo, not of the invocation — a flag makes every render site (a shell, a
hook, CI, a teammate's terminal) a place the answer can be given differently, and the one
that forgets silently writes half the layout. The manifest is committed, so the answer is
the same everywhere and reviewable.

**No default, refuse if unset** — the rule `MemoryConfig.Agent` and `Reviewer.Provider`
already follow. Rejected here, and the difference is what those two gate: an operation that
spends money on a model the consumer has to have chosen deliberately. Rendering is free and
local, and claude-only is already every existing repo's behavior, so an unset `platforms:`
has an answer that is both obvious and identical to today. There is nothing to refuse.

**Silently ignoring a platform with no adapter.** Rejected. `cursor`, `copilot`, `opencode`
and `agents` are real `definitions.Platform` values, so a stack naming one in `platforms:`
parses — and would then render nothing at all, with the consumer's evidence being an empty
directory. The parser rejects a name that is not a Platform at all; the render dispatch
rejects a Platform with no adapter behind it. Both are errors that say which name failed.
The same values stay valid on a *definition's* own `platforms:`, which narrows where a
definition may go rather than naming a render target — a Claude-only skill declaring
`platforms: [claude]` is unaffected by any of this.

**Leaving `AGENTS.md` to its author, and seeding `CLAUDE.md` from it.** This is the shape
that existed: when a project root already held an `AGENTS.md`, the Claude adapter wrote
`CLAUDE.md` as an `@AGENTS.md` import rather than as instruction bodies. Rejected, because
Codex reads `AGENTS.md` as its instruction surface and its rules index — agtk has to own the
file to render for Codex at all, and a file agtk owns is not one to defer to. The two files
are now built independently from the same `instructions:` data, so neither adapter reads the
other's output and there is no render order between them.

**Skipping `agents:` and `commands:` on Codex.** This was the first shape of this change and
it was wrong on both rows. Codex subagents are an authored construct with a documented TOML
file, and while Codex retired its custom-prompt files, a skill is a command's near-equivalent.
Every category has a real Codex destination, so nothing renders as a warning.

**A render-time error when a `commands:` name collides with a `skills:` name.** Both convert
onto `.agents/skills/<name>/SKILL.md`, so on Codex they are the same file. Rejected in favour
of the resolution Claude Code already applies to a skill and a command sharing a name: the
skill wins, the command is dropped. Inventing a second rule for the identical collision would
make the same pair of definitions mean different things on the two platforms for no reason,
and an error would fail a render that is fine on Claude.

## Consequences

- A consumer repo that relied on the `@AGENTS.md` seed gets a different `CLAUDE.md`: its
  managed region now holds the instruction bodies directly instead of an import line. The
  region's boundaries are unchanged, so content outside the markers is preserved as before.
  A repo whose `AGENTS.md` is hand-authored and which then opts into `codex` has agtk take
  that file over — the first render refuses on the collision, as it does for any untracked
  file, until `--force`.
- Codex's two roots do not nest: skills and rules under `.agents/`, subagents under
  `.codex/agents/`. The manifest sits at `.agents/.agtk-manifest.json` but keys every path
  relative to the project root, so one manifest still detects a file stranded under either
  root. Claude's stays scope-root-relative; the divergence is the price of a layout that is
  not a single tree.
- `mcp:`, `settings:` and `hooks:` share one file on Codex, `.codex/config.toml`, where
  Claude spends `settings.json` and `.mcp.json`. Ownership is recorded there per dotted key
  path rather than per top-level key, because the flag that turns hooks on is one key inside
  a `[features]` table a consumer may also be using — reclaiming the whole table to release
  one key would take their flags with it. Claims are compared segment-wise for the same
  reason: a `settings:` fragment naming `features` would pass a string-equality check against
  `features.hooks` and replace the table the flag lives in, which leaves every rendered hook
  in the file and none of them running. The fragment is dropped instead, the precedence
  `settings.json` already gives hooks over settings.
- A hook agtk writes is one Codex would not run unless hooks are enabled, so the adapter
  sets `features.hooks` itself. It is written and released with the hooks, not left as a
  step a consumer has to know about.
- Two canonical values have no Codex destination and are reported rather than written: a
  `prompt` handler, which Codex parses and never executes, and an `sse` transport, which
  Codex has no client for. Both are skipped per definition and the render continues — the
  same definition is usually rendering correctly for Claude in the same pass. A dry run
  reports them too, because it decides what it says about `config.toml` by running the same
  collectors the render does rather than by reading which categories the plan holds.
  `fail_closed` is not reported, because a Codex hook blocks by its exit code and there is
  no key it belongs in.
- A converted command carries its "only on an explicit user request" restriction as the
  first line of its body. Codex's `SKILL.md` frontmatter has no equivalent of Claude's
  `disable-model-invocation`, so the restriction is prose or it is nothing.
- A definition's own `platforms:` allowlist starts being enforced, because it finally decides
  something. The field predates this change and the schema always described it as a narrowing
  list, but with one render target there was nothing to narrow against, and nothing read it.
  With two, it is what keeps one platform's vocabulary out of another's files: a `permissions`
  block in Claude's matcher syntax, or a hook shelling out to `--client=claude-code`, is not a
  thing Codex rejects — it is a thing Codex neither reads nor complains about. Narrowing is
  applied once in the render dispatch rather than inside an adapter, since the allowlist is a
  property of the definition and not of any one layout.
- Narrowing is the wrong tool where a definition is *almost* portable, so `CodexMCPExt` gains
  an `args` override. A server that is told which client it serves differs between the two
  platforms by one flag's value, and the alternatives were both worse: a `serena-claude` and a
  `serena-codex` rename the server, because a definition's name is the key it is addressed by
  and consumers hold `mcp__serena__*` permission patterns against it; leaving one argv for both
  starts the server in the wrong client's context. The override replaces the canonical argv
  rather than extending it — appending would leave the flag carrying two values.
- `.codex/config.toml` is removed rather than left empty when the last key agtk owned goes and
  the consumer had none of their own in it. A nought-byte file is one Codex still parses and
  one more line in every diff.
- `AGENTS.md` carries an index of `rules:` — description and relative link — that `CLAUDE.md`
  does not. Codex has no rules-discovery mechanism, so without the index a rule file is
  written where nothing will ever read it.
- Two properties of the shared machinery are the whole point of sharing it, and they reach
  the Claude adapter as much as the Codex one. Stale removal refuses a manifest entry that
  resolves outside the render root, lexically or through a symlinked ancestor: the manifest
  is committed, so its keys are input to a render rather than something the render wrote, and
  an entry carrying `..` — or sitting behind a symlink committed beside it — turns a render
  into a delete of anything the invoking user can write. And a dry run fails on a
  mixed-ownership file that will not parse, because `Options.DryRun` says errors depending on
  filesystem state are surfaced, and a preview that exits clean before a render that aborts
  on the same file is previewing a different render.
