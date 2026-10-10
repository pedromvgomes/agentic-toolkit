---
about: "ParseFile takes each message's row from its last usable record, and holds only the file's final assistant message when none of its records carries a stop_reason"
saw:
  - source/toolkit/internal/usage/parse.go
---

`stop_reason` is not a reliable completion signal in real transcripts (most finished messages never carry
a non-null one), so `ParseFile` calls a message complete when any of its records has one, or when an
assistant record with a different message id follows it. `lastID` is the id of the last assistant record
that carries a message id, usable or not, and only that message can be held, so `Result.Held` has at most
one entry.

The winning record is the last *usable* one: a record with no usage or the `<synthetic>` model is counted
in `Skipped` and never becomes the winner, though its `stop_reason` still marks the message stopped. A
message whose records are all unusable produces no row and is never held.
