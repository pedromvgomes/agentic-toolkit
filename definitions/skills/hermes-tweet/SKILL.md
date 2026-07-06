---
name: hermes-tweet
description: Use Hermes Tweet with Xquik for X/Twitter research, read workflows, social monitoring, and approval-gated social actions from agent sessions.
tags:
  - hermes-agent
  - xquik
  - x-twitter
  - social-media
  - automation
---

# Hermes Tweet

Use this skill when a repository or agent session needs X/Twitter context through the Hermes Tweet plugin and Xquik.

## When to Use

Use Hermes Tweet for social listening, launch monitoring, creator research, brand research, giveaway audits, community audits, and user-approved publishing workflows.

## Setup

Install Hermes Tweet from the source repository:

```sh
hermes plugins install Xquik-dev/hermes-tweet
hermes plugins enable hermes-tweet
```

Configure `XQUIK_API_KEY` in the Hermes runtime environment before read tools are expected to work. Keep `HERMES_TWEET_ENABLE_ACTIONS` unset or false until the session explicitly needs account-changing actions.

## Workflow

1. Start with endpoint discovery or read-only research.
2. Prefer read workflows for monitoring, summaries, and audits.
3. Use action workflows only after the user states the exact operation and confirms the side effect.
4. Keep keys and credentials in the runtime environment, not in prompts, code, examples, or issue text.

## Safety

- Never ask for API key values in chat.
- Do not pass credentials as tool arguments.
- Do not guess X/Twitter endpoint paths.
- Do not retry writes through alternate routes after a policy, auth, or account-state failure.
