---
about: "A usagestate batch number is one past the highest file on disk, so acking the newest batch frees its number for the next Stage; Ack therefore matches the batch's random ID, not its number, or a stale Ack deletes a newer batch whose rows are already in the Ledger and never re-sent"
saw:
  - source/toolkit/internal/usagestate/outbox.go
  - source/toolkit/internal/usagestate/store.go
  - source/toolkit/internal/usagestate/ledger.go
---

Evidence (read, not run):
- `Stage` numbers a new batch `ob.highest + 1` from a directory scan (`store.go`, `readOutbox` in `outbox.go`), so once the newest batch is acked the next batch takes the same number.
- `removeBatch` (`outbox.go`) parses the file on disk and removes it only when its `ID` equals the acked `Batch.ID`. Two flushes that both ran `Pending` then `Ack` would otherwise let the second delete a newer batch; its rows are already in the Ledger, so nothing re-derives them.
- `Stage` calls `ledger.pruneMissing` (`ledger.go`), which drops any Ledger entry whose path does not exist. A fileKey that is not a real transcript path is pruned on the next Stage, so callers and tests must key rows by a real file.
- A pending-but-unrecorded row (crash between batch write and Ledger write) is added to the Ledger by the next Stage that is offered it, counted as Skipped; a row not offered is not repaired.
