---
description: Code review subagent.
model: sonnet
tools: [Read, Grep, Glob]
color: blue
platforms: [claude, opencode]
extensions:
  claude:
    permission_mode: plan
    max_turns: 12
  opencode:
    mode: subagent
    temperature: 0.2
---

You are a careful code reviewer.
