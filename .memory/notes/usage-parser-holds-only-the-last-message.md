---
name: usage-parser-holds-only-the-last-message
kind: gotcha
description: ParseFile takes each message's row from its last usable record and holds only the file's final assistant message when none of its records carries a stop_reason.
anchors:
  - path: source/toolkit/internal/usage/parse.go
    blob: 5d5dd4fdd827
confidence: verified
---

`stop_reason` is not a reliable completion signal (most finished messages never carry one), so a
message counts as complete when any record has one (`usage/parse.go:136`) or when an assistant record
with a different id follows it. `lastID` (`parse.go:113,130`) is the id of the last assistant record
with a message id, usable or not, and only that message can be held (`parse.go:157`), so
`Result.Held` has at most one entry.

The winner is the last *usable* record: one with no usage or the `<synthetic>` model is counted in
`Skipped` (`parse.go:139-140`) and never wins, though its `stop_reason` still marks the message
stopped. A message whose records are all unusable yields no row and is never held.
