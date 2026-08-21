---
description: Comments state what the code does and why, never what it used to do or which change introduced it.
---

## Comments describe the code, not its history

A comment is read by someone looking at the code as it is now, who never saw an earlier
version and has no reason to care. The commit message, the PR description and `git log`
are where a change is explained. A comment that narrates a change is stale the moment the
next one lands.

This applies to every comment and docstring in the repo — source, tests, fixtures, config,
markdown — and to test names, which say what behaviour they protect, not which bug prompted
them. ADRs are the one exception: recording the rejected alternative *is* the decision's
content. Even there, do not narrate which PR changed what.

**Never write**

- PR/ticket numbers — `(PR 4)`, `TICKET-113 fixed this`
- `used to` / `previously` / `no longer` / `an earlier version of this`
- `Regression:` preambles, or corrections addressed to a past author
- Roadmap and lifecycle — `arrives later`, `this is new`, `for now`, `will be replaced by`
- Change bookkeeping — `Added`, `Updated`, `Moved from X`, `Renamed`

**Instead** state the rule, the invariant, or the failure mode in the present tense, as a
property of the code:

```
// ✗ Regression: toString() previously keyed off a `dirty` flag nothing ever set, so it
//   always replayed the CST and discarded AST edits.
// ✓ toString() detects edits by comparing serialisations. A `dirty` flag a caller has to
//   remember to set means the CST is replayed and every AST edit is silently discarded.
```

Keep the mechanics, drop the history: *"without this, X happens"* is a property of the code;
*"this used to do X"* is a changelog entry.

**The test:** could a reader who has never seen an earlier version of this file act on the
comment? If it only makes sense as a diff against something no longer there, rewrite it.
