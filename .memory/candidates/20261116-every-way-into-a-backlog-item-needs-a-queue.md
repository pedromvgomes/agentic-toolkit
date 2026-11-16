---
about: "agtk backlog verify refuses any item not carrying agtk:queued, so a person running /implement-backlog-item by hand also needs the item queued first; queuing is the only way in, whoever then runs it"
saw:
  - source/toolkit/internal/backlog/verify.go
  - definitions/skills/implement-backlog-item/SKILL.md
  - definitions/commands/plan-feature.md
  - definitions/skills/write-backlog-item/SKILL.md
---

`Verify` in `internal/backlog/verify.go` refuses with `ReasonNotQueued` (`not-queued`) when the
issue's labels lack `LabelQueued`, before it reads who wrote the body. There is no interactive
exemption: `implement-backlog-item` step 2 runs `agtk backlog verify` in both modes and stops on
any refusal. So "run `/implement-backlog-item owner/repo#N`" and "add `agtk:queued`" are not two
alternative ways in: an unqueued item refuses either way. `plan-feature` step 6 and
`write-backlog-item` step 8 word it as "a person with write access reads and queues it; then they
run it or a Factory dispatches it". Queuing an item therefore also hands it to any Factory
watching the repository.

The same check means the Factory must leave `agtk:queued` on until the session it starts has
verified: swapping it for `agtk:running` first makes every unattended run refuse.
