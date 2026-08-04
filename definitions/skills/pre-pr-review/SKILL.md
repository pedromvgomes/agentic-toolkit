---
name: pre-pr-review
description: |
  Multi-agent pre-PR code review of the current worktree's changes vs the branch's source. Partitions the diff by language, selects a
  stack-specific reviewer panel (Kotlin/Spring, React, Go, Rust, or a language-agnostic `general` panel) from `agents.json` for each one,
  fans every specialist out in parallel, then consolidates findings into a RED/AMBER/GREEN triage table so the user can decide what to fix
  before opening the PR. Handles polyglot monorepo branches in a single run. Trigger on phrases like "review my branch", "pre-PR review",
  "review before PR", "check my changes before I open the PR", or "run a code review on this branch".
---

# Pre-PR multi-agent code review
Review the current worktree's changes against the branch's **source (parent) branch** using stack-specific panels of specialist subagents, then
triage their findings as RED / AMBER / GREEN so the user can pick what to fix before opening a PR.

The diff is partitioned by language first. A branch that touches several stacks gets one panel per stack in the **same** run — all subagents
fan out in parallel and their findings land in one triage table. A branch that touches none of the defined stacks (scripts, CI, infra, docs)
gets the language-agnostic `general` panel rather than no review at all.

In a stacked-branch workflow the source branch is often another feature branch, not `main`. The skill discovers the parent with plain `git` and lets
the user override the detected value before the review starts.

The subagent roster lives in `agents.json` next to this file, keyed by stack. The file has two top-level maps: `detect` (per-stack
auto-detection hints) and `agents` (per-stack reviewer rosters). Each roster entry declares `name`, `model`, `prompt_file` (relative to the
skill dir), and a short `description`. To add a stack, retune an axis, change models, or adjust auto-detection, edit `agents.json` or the
relevant prompt file under `prompts/<stack>/` — do **not** edit this SKILL.md.

## Helper scripts

This skill ships helper scripts under `scripts/` next to this SKILL.md. The skill runs from the *target repo's* working directory, not its
own, so before running any of them resolve the skill's own directory once and hold it in a shell variable:

```bash
SKILL_DIR="<absolute path of the directory containing this SKILL.md>"   # e.g. ~/.claude/skills/pre-pr-review
```

Every `scripts/...` invocation below is written as `"$SKILL_DIR/scripts/..."` and assumes that variable is set.

## Workflow

### Phase 1 — Detect parent, gather diff, partition by stack, confirm scope
Do all of the following before launching any subagent. Steps 1.1–1.3 run without user interaction; a **single** `AskUserQuestion` call
at 1.4 confirms all three decisions at once.

#### 1.1 Detect the parent branch
Run the parent-detection script:

    bash "$SKILL_DIR/scripts/detect-parent.sh"

It returns JSON like `{"current":"...","parent":"...","base":"...","candidates":[...]}`. Capture `base` into `$BASE` — the diff scripts below
take it as their first argument. If the script returns an object with an `error` key instead, report the message and ask the user for a base
ref before continuing.

#### 1.2 Gather the diff
Capture the full set of changes — committed-on-branch vs parent, all worktree changes (staged and unstaged together), and untracked
files — with one script, so nothing on the worktree is missed:

    bash "$SKILL_DIR/scripts/capture-diff.sh" "$BASE"

Then collect the matching changed-files list so subagents can `Read` full-file context when a hunk alone is ambiguous:

    bash "$SKILL_DIR/scripts/list-changed.sh" "$BASE"

Both scripts take the same arguments and emit the same three sections in the same order, so the diff and the file list always agree.
Both also accept optional pathspecs after `$BASE`: a `!`-prefixed pathspec excludes matching files, any other pathspec restricts output
to matching files. A pathspec with no `/` is treated as a basename and matches at any depth, so `'!package-lock.json'` drops every
lockfile in a monorepo, not just the root one; write a path containing a slash to anchor at the repo root. These are used below to drop
excluded files (1.4) and to scope each panel to its own language (Phase 2).

If the combined diff is empty, stop and tell the user there is nothing to review.

#### 1.3 Partition the diff by stack
Read `agents.json`. The keys under `agents` are the available stacks; the `detect` map holds each stack's auto-detection hints. Both are
data — new stacks are added by editing `agents.json`, never this file.

**Assign every changed file to a stack.** For each stack in `detect`, a file matches when it satisfies any of that stack's hints:
- `extensions` — the file's extension is in the list.
- `files` — the file's basename equals an entry (e.g. `go.mod`, `Cargo.toml`).
- `path_contains` — the file path contains one of the substrings (e.g. `src/main/kotlin`, `src/components`).

A stack with no `detect` entry never auto-matches — it's still selectable manually. This is why `general` never claims files on its own.

Sort the file assignments into buckets:
- One bucket per stack that matched at least one file.
- An **unclaimed** bucket for files no stack matched: shell scripts, CI workflows, Dockerfiles, IaC, docs, and languages with no roster.

A bucket is **significant** if it holds at least 3 files, or at least 20% of the reviewable files. Small buckets below that bar get folded
into the largest significant bucket as context rather than getting their own panel — a monorepo change that touches 40 Kotlin files and one
`.tsx` doesn't need a React panel.

This partition drives Phase 2: **one reviewer panel per significant bucket, all fanned out in parallel in a single invocation.** The skill
is not limited to one language per run — a monorepo change spanning Kotlin and React runs both rosters and consolidates their findings into
one triage table. Do not ask the user to run the review twice.

Resulting shape:
- **One significant bucket** → one panel, that stack. If the only bucket is the unclaimed one, use `general`, whose reviewers are
  language-agnostic. Say which stack in one line.
- **Several significant buckets** → recommend running a panel for each, and say so in the 1.4 question with the per-bucket file counts.
  Offer "only the dominant stack" as the cheaper alternative.
- **A significant unclaimed bucket alongside language buckets** → give it a `general` panel too, so CI, scripts, and infra changes are
  reviewed rather than silently skipped.
- **Only one stack defined** in `agents.json` → use it silently, just tell the user which one (one line).

Cost note: each panel is 2–3 subagents. Before recommending more than three panels, say what that costs in the question preamble and let the
user narrow it.

#### 1.4 Confirm parent, panels, and scope — one gate
Write a short prose paragraph (not a file list) describing the change at a high level — what subsystems it touches, roughly how many
files, structural moves. Example shape:

> *"Reviewing against detected parent `feature/api-base`. This branch restructures `common-logging` as an independent module: ~14 files moved
> into `modules/common-logging/`, plus migrations of two integration tests. Build config updates in `libs.versions.toml`, and a new release
> workflow under `.github/workflows/`. That splits into two panels: `kotlin-spring` (16 files) and `general` for the CI/script changes
> (3 files). No vendored or generated files detected."*

Then call out any files that probably should **not** be reviewed by the subagents. Heuristics for "likely exclude":

- Lock files: `*.lock`, `package-lock.json`, `yarn.lock`, `pnpm-lock.yaml`, `Cargo.lock`, `gradle.lockfile`.
- Generated code: paths under `build/`, `generated/`, `target/`, `dist/`, `.next/`, `node_modules/`, or files with a "DO NOT EDIT" header.
- Large binary or data files (non-text, or > ~500 KB).
- Snapshot / fixture files: `*.snap`, large `*.json` test fixtures.
- Vendored third-party code: `vendor/`, `third_party/`.
- Diffs that are purely whitespace / formatting / rename-only.

If nothing matches, state "no exclusion candidates detected".

Now issue **one** `AskUserQuestion` call carrying every open decision as a separate question. Omit any question that has only one sensible
answer (a single stack in `agents.json`, one significant bucket, or no exclusion candidates found) rather than asking it:

1. **Base branch** — *Use detected parent (`<PARENT>`)* (recommended) / *Use `<default branch>`* (only if the detected parent isn't already
   the default) / user types another branch.
2. **Panels** — *Run all detected panels (`<stack-a>` + `<stack-b>`)* (recommended when several buckets are significant) / *Only
   `<dominant stack>`* / another single stack from `agents.json`.
3. **Scope** — *Review everything* / *Exclude the flagged files* (only offered if you flagged some; list them in the option description).

If the user overrides the base branch, recompute `$BASE` with `git merge-base HEAD <branch>`, re-run both scripts from 1.2, redo the
partition, and re-ask only the panel/scope questions if the new diff changes the answers materially. Otherwise carry the first answers
forward.

If the user chose to exclude files, re-run **both** scripts with the exclusions appended as `!`-prefixed arguments rather than editing the
diff text by hand:

    bash "$SKILL_DIR/scripts/capture-diff.sh" "$BASE" '!package-lock.json' '!vendor/**'
    bash "$SKILL_DIR/scripts/list-changed.sh" "$BASE" '!package-lock.json' '!vendor/**'

Do not start Phase 2 until the user has answered.

### Phase 1.5 — Extract repo conventions (single shared pass)
1. Determine which modules the diff touches (parse the changed-files list and extract distinct module roots), then run:

   bash "$SKILL_DIR/scripts/find-convention-docs.sh" <module-paths...>

   It returns a JSON array of doc paths that exist. If empty, skip this phase. Tell the user "no convention docs found — reviewers will
   use general best practices for the <stack> stack." Proceed directly to Phase 2 with an empty conventions summary.

2. Otherwise, launch one `Agent` call:
   - `subagent_type: "general-purpose"`
   - `model: "sonnet"`
   - `description: "Pre-PR review: extract conventions"`
   - `prompt:` the extractor prompt below, with the list of doc paths that exist and the changed-files list appended.

3. The extractor returns a markdown summary structured as a bulleted list of rules, each with a source citation. Expected shape:

   ```markdown
      ## Repo conventions extracted from docs
   
      ### Root-level
      - **Unit tests run via the `unitTest` gradle task, not `test`.**
        Source: `docs/CODE_STANDARDS.md` §Testing.
      - **URL pattern matching uses `PathPatternParser`, not `AntPathMatcher`.**
        Source: `docs/ARCHITECTURE.md` §Routing.
   
      ### Module: `modules/common-logging`
      - **All filters extend `AbstractLoggingFilter`.**
        Source: `modules/common-logging/AGENTS.md`.
   
      ## Conventions not extracted (out of scope for this diff)
      - Build pipeline rules — diff doesn't touch CI config.
   ```

4. If the extractor returns an empty list (docs exist but contained no rules the diff could violate), proceed with an empty summary.

5. **Show the user the extracted conventions before fan-out.** Render the summary as-is (it's already markdown) under a short framing line:

   > *"Here are the conventions reviewers will be held against. Anything missing, wrong, or in scope for this diff that I should drop before
   > launching the panel?"*

   Use `AskUserQuestion` with three options:
   - **Use as-is** — keep this summary.
   - **Edit the summary** — user types corrections; apply them inline and show the revised summary, then re-ask.
   - **Skip conventions entirely** — drop to an empty summary (reviewers rely on Scope alone).

   This gate is especially valuable on the first run against a new repo, when extraction quality is unverified. On repeated runs against the
   same repo, the user can move past it quickly.

6. Pass the confirmed conventions summary into every Phase 2 agent prompt as a new section between the shared preamble and the per-axis prompt
   body. If the summary is empty (skipped, or no rules found), omit the section entirely rather than including an empty placeholder.

#### Extractor prompt
> You are extracting repo-specific conventions from documentation files
> so that downstream code reviewers can hold a diff against them. You are
> not reviewing code yourself.
>
> You will receive:
> - A list of doc file paths that exist in this repo (at the root and at module roots).
> - The changed-files list for the diff under review.
>
> Procedure:
>
> 1. `Read` each listed doc file.
> 2. Extract every rule that is (a) prescriptive (uses words like "must," "always," "never," "do not," or is structured as a hard rule rather
>    than a recommendation) AND (b) could plausibly be violated by code in the changed-files list. Skip rules about parts of the codebase
>    not touched by the diff. Skip aspirational language, historical context, and pure-style preferences (indentation, line length, import ordering).
> 3. For each rule, record:
>    - The rule itself, in one sentence, in the doc's own terms.
>    - The source: file path plus section heading or line range.
>    - The scope: repo-wide, or specific module path.
> 4. Output the markdown summary in the structure the orchestrator specified. Group rules by scope (root-level first, then by module).
>    Add a brief "Conventions not extracted" section listing major rule categories you skipped and why, so the orchestrator can confirm
>    nothing relevant was missed.
>
> If a doc contains no rules that the diff could violate, return an empty list under that scope. Returning an entirely empty summary is a
> valid outcome.
>
> Do not infer conventions that aren't written in the docs. Do not include rules whose source you can't cite. Do not paraphrase rules in
> ways that change their meaning — quote the doc's wording where it matters.

### Phase 2 — Fan out the specialist subagents in parallel
1. From the parsed `agents.json`, select `agents[<stack>]` for **each** confirmed panel. For every entry, read the markdown body at
   `prompt_file` (path relative to the skill dir).

2. **Scope each panel's diff to its own bucket.** Re-run both Phase 1.2 scripts once per panel, passing that bucket's files as include
   pathspecs (plus any exclusions from 1.4), so a Kotlin reviewer isn't reading React files and vice versa:

       bash "$SKILL_DIR/scripts/capture-diff.sh" "$BASE" '**/*.kt' '**/*.kts' 'gradle/libs.versions.toml' '!vendor/**'

   Use globs where the bucket is cleanly separable by extension or directory, and explicit paths where it isn't. Every reviewer in a panel
   gets the same scoped diff. With only one panel, skip this and reuse the Phase 1.4 output unchanged.

   Files that landed in a small non-significant bucket (folded in per 1.3) go to the panel they were folded into.

3. Announce what will run in one line per panel, including the agent descriptions from `agents.json` — gives the user a quick sense of what's
   being checked. Example:
   > *"Launching 5 reviewers: `kotlin-spring` (performance, security, best-practices) over 16 files, `general` (security, best-practices) over
   > the 3 CI/script files."*

4. For every agent in every selected roster, launch one `Agent` call with:
   - `subagent_type: "general-purpose"`
   - `model:` the value from the roster (`"opus"` / `"sonnet"` / `"haiku"`)
   - `description:` `"Pre-PR review: <stack>/<agent.name>"`
   - `prompt:` the assembled prompt described below.

   **All `Agent` calls MUST be issued in a single message** so they execute in parallel — including across panels. Do not run one panel,
   wait, then run the next.

   When more than one panel is running, tell each agent its panel's stack name and instruct it to prefix its `category` values with that
   stack (e.g. `kotlin-spring/security:injection`), so Phase 3 can tell converging panels apart.

5. The prompt passed to each subagent is the concatenation of:

   ```
      <shared preamble — copy verbatim from the section below>
   
      ---
   
      ## Repo conventions extracted from docs
   
      <the conventions summary returned by the Phase 1.5 extractor agent>
   
      (Omit this section entirely if the extractor returned an empty summary or Phase 1.5 was skipped.)
   
      ---
   
      <contents of the agent's prompt_file>
   
      ---
   
      ## Changed files
   
      <output of `list-changed.sh` for this panel's scope (step 2)>
   
      ## Unified diff
   
      <output of `capture-diff.sh` for this panel's scope (step 2)>
   ```
   
   When several panels are running, add one line above the changed-files section telling the agent that the diff has been scoped to its
   stack, that sibling panels cover the rest of the branch, and that it may still `Read` any file in the repo for context.
   
   #### Shared preamble (use verbatim in every subagent prompt)
   
   > You are one of several specialist reviewers running in parallel as part of a multi-agent code review panel. Other reviewers cover the axes you are
   > told to ignore — do not duplicate their work. If an issue spans multiple axes, file only your axis and note the others in one line so the
   > coordinator can dedupe.
   >
   > If a "Repo conventions extracted from docs" section appears below, treat it as the authoritative source for repo-specific rules. Hold the diff
   > against those rules and cite them by their source when filing convention findings. If the section is absent, no convention docs were found — rely
   > only on your axis's scope, do not invent repo conventions.
   >
   > Return findings in this exact YAML-ish schema, one entry per finding, nothing else outside the list:
   >
   > ```yaml
   > - severity: RED | AMBER | GREEN
   >   category: <your axis, e.g. "security:injection" or "perf:n+1">
   >   file: path/to/file.kt
   >   line: <line or range, or "n/a" if cross-cutting>
   >   issue: <one-sentence description of the problem>
   >   evidence: <short quote or reference from the diff/file; for convention
   >             findings, also quote the rule and its source from the
   >             conventions summary>
   >   proposed_action: <concrete fix, not a vague suggestion>
   >   confidence: high | medium | low
   > ```
   >
   > Severity calibration (applies identically across all reviewers and overrides any conflicting bar in your axis prompt):
   > - **RED** — must fix before merge: real bug, exploitable vuln, data loss, significant perf regression on a hot path, breaks a documented contract.
   > - **AMBER** — should fix: latent risk, maintainability problem, minor perf issue, convention violation with real downstream cost.
   > - **GREEN** — nice to have: nit, style, opportunistic improvement.
   >
   > Every finding must quote the offending line(s) in `evidence`. If you can't point to specific code, don't file it. Returning an empty list is a
   > valid outcome — false positives are more costly than misses.

### Phase 3 — Consolidate

When all subagents have returned:

1. Parse every agent's finding list, from every panel.
2. **Deduplicate** — when two agents flag the same `file` + `line` for the same underlying issue, keep the higher severity and merge their 
   `proposed_action` text. Preserve both `category` values (comma-joined) so the user sees which axes converged. Panels review disjoint file
   sets, so most duplicates come from sibling axes within one panel; a cross-panel duplicate means a shared file (config, CI) landed in two
   scopes — merge it the same way.
3. **Demote unconfident findings** — a RED with `confidence: low` moves to AMBER. A finding a reviewer isn't sure about shouldn't
   read as a merge blocker. Two agents independently reporting the same issue (a dedupe merge in step 2) counts as corroboration —
   don't demote those.
4. **Drop noise** — silently discard any GREEN finding with `confidence: low`.
5. **Sort** by severity (RED → AMBER → GREEN), then within a severity by file path so related findings cluster. Sort last, after every
   severity has settled, so demoted findings land in file order rather than appended to the end of their new section.
6. Number the surviving findings continuously across severities (1, 2, 3 …) so the user can refer to them in a follow-up turn.

### Phase 4 — Present the triage

Render three sections, one per severity, each a markdown table. Omit a section entirely if it would be empty. The `Conf` column carries
the finding's `confidence` so the user can weigh a borderline call without asking; mark a deduped finding (step 2) as `high (2 agents)`
and a demoted one (step 3) as `low (demoted)`.

```markdown
## RED — must fix before PR
| # | Category           | Location             | Conf | Issue                                       | Proposed action                            |
|---|--------------------|----------------------|------|---------------------------------------------|--------------------------------------------|
| 1 | security:injection | UserController.kt:42 | high | Unparameterized SQL built from request body | Switch to JdbcTemplate parameterized query |

## AMBER — should fix
| # | Category | Location | Conf | Issue | Proposed action |
| ... |

## GREEN — nice to have
| # | Category | Location | Conf | Issue | Proposed action |
| ... |
```

After the tables, ask the user:

> *"Tell me which findings to fix (e.g. 'all RED', 'fix 1, 4, 7', 'skip all'). I won't modify code without your explicit selection."*

### Phase 5 — Apply selected fixes (only if the user picks any)

Implement just the selected findings using the same diligence as any other edit task. After editing, run the project's own checks — prefer
a command the repo documents (in its README, contributing guide, or the conventions summary from Phase 1.5) or one that already exists in
its CI config, since a project's test task is often not the language default. Fall back to the stack's conventional command only when the
repo names none (`gradle test` for Kotlin/Spring, `npm test` / `pnpm test` for React, `cargo test` + `cargo clippy` for Rust,
`go test ./...` + `go vet ./...` for Go). Report what was changed and which findings remain unaddressed.