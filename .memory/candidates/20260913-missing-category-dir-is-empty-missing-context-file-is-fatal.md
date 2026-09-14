---
about: a missing per-category scan directory under root: is silently empty, but a configured-but-missing context: file is a hard error — deliberately asymmetric
saw:
  - source/toolkit/internal/resolver/entryscan.go
---

`scanCategory` (`entryscan.go`) treats `fs.ErrNotExist` on a category directory as "nothing
found," with no diagnostic and no error — a repo that never created `agentic/agents/` is not
making a configuration mistake, it just has no agents. `scanEntryRoot`'s handling of `context:`
is the opposite: when `m.Context != ""`, a read failure on that named file is appended to
`s.errs` and fails resolution. The asymmetry is deliberate (not an oversight to unify) — a
category directory is opt-in-by-existence under an always-on convention, while `context:` is a
single field the consumer explicitly configured, so its absence means the repo described
content it does not have.
