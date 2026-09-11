---
name: example
description: An example skill used by parser tests.
platforms: [claude, cursor]
tags: [demo]
extensions:
  claude:
    allowed_tools: [Read, Grep]
    argument_hint: "<goal>"
    disable_model_invocation: true
---

# Example skill

Body of the skill.
