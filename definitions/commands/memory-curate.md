---
description: Promote, merge or reject the findings staged in the memory store's candidates/, then regenerate the index.
argument_hint: "[--stale]"
tools: [Bash]
---

Run the curator over the memory store's staged findings:

```bash
agtk memory curate $ARGUMENTS
```

The curator runs in its own process with its own context, and is fed the candidates plus
the matching slice of the index — never this session's transcript. That is deliberate:
asking for merge-and-reject judgment at the moment context is most exhausted is how
curation quietly stops happening.

Report what it printed. Do not curate by hand, and do not edit anything under the store's
`notes/` or its `INDEX.md`: `notes/` has exactly one writer and this command is how it
runs. `INDEX.md` is generated.

If `agtk memory curate` reports that no provider is configured, the repo has not chosen
one — say so and stop. The provider is named by `memory.agent` in the entry manifest, and
choosing it is the repo's decision, not this session's.

To see what is waiting without curating anything, `agtk memory candidates` reports the
backlog and never invokes a model.
