---
about: "settings `value` keys pass through the claude adapter unvalidated, contradicting SCHEMA.md; and the footer guard only judges tool_name Bash"
saw:
  - source/toolkit/internal/adapters/claude/settings.go
  - definitions/SCHEMA.md
  - source/toolkit/internal/guard/footers.go
  - definitions/hooks/no-authoring-footers-claude-pre-tool-use.yaml
  - stacks/default.yaml
---

1. `definitions/SCHEMA.md:442` says adapters validate settings keys and skip-with-warn on unsupported ones.
   `collectSettingFragments` (`adapters/claude/settings.go:~431-478`) copies every top-level key of every
   setting `Value` into the output map (`out[k] = v`) with no vocabulary check; `grep -in
   'unsupported|allowed|knownKeys|vocabulary' adapters/claude/*.go` finds no key validation. So any key
   (e.g. `attribution`, `includeCoAuthoredBy`) is expressible and written verbatim to `.claude/settings.json`,
   marked managed in `_meta.agtk.managed`. Only `hooks` (hook renderer wins) and `permissions` (unioned) are special.
   No existing definition under `definitions/settings/` (feature-flow-model: `model`; memory-permissions and
   skill-permissions: `permissions.allow`) sets either key; `grep -ri 'attribution|includeCoAuthoredBy'` over
   definitions/stacks hits nothing.
2. The guard hook has `matcher: Bash` and `DecideFooters` (`guard/footers.go:~795`) allows when
   `hook.ToolName != "Bash"`, so `mcp__github__*` PR/comment/review/issue tools are never inspected.
   Covered inside Bash only: git commit/tag, gh pr/issue/release, gh api. Blind spots (v0.15.0 release notes):
   env-sourced values, earlier shell calls, `bash script.sh`. The hook is fail-open (malformed JSON allows;
   missing/older agtk exits 0). Only `stacks/default.yaml` lists the hook and instruction (lines ~44, ~49).
