# Skill attribution probe

Hooks on every event plus a `probe` skill and a `probecmd` command that print what a Bash call,
a process it spawns, and a subagent's Bash call can see. It answers which hook events fire for a
skill, what env reaches each layer, and how subagent and nested-session transcripts are linked.

## Run

From this directory:

```bash
SPAN_PROBE=outer claude -p "/probe" --output-format json --permission-mode acceptEdits \
  --allowedTools "Bash(printenv:*)" "Bash(sh:*)" Agent
claude -p "Invoke the probe skill using the Skill tool, then say done." \
  --session-id "$(uuidgen)" --output-format json --permission-mode acceptEdits \
  --allowedTools "Bash(printenv:*)" "Bash(sh:*)" Agent Skill
```

Then read:

- `hooks.log`: one block per event, with the payload and the env the hook saw.
- `~/.claude/projects/<mangled cwd>/<session id>.jsonl` and `<session id>/subagents/`: look for
  `attributionSkill`, `isSidechain`, `agentId`, `promptId`, `sessionId` and `entrypoint`.

Run it without `--session-id` from inside another Claude Code session to see whether the child
adopts the parent's `CLAUDE_CODE_SESSION_ID`.
