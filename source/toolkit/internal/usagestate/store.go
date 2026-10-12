package usagestate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pedromvgomes/agentic-toolkit/internal/usage"
)

// Hooks is a test seam: it lets a test interrupt a Store between the steps of
// an operation. Production callers use Open, which sets none.
type Hooks struct {
	// AfterBatchWrite runs in Stage once the batch file is written and before
	// the Ledger is. A non-nil error makes Stage return it there, leaving the
	// rows in the Outbox but not the Ledger, as a crash between the two would.
	AfterBatchWrite func() error
}

// Store is the Ledger and Outbox in one state directory. Every method takes
// the directory's lock for its whole duration, so concurrent callers in this
// process or any other see each operation whole.
type Store struct {
	dir   string
	hooks Hooks
}

// Open returns the Store in dir, creating dir when absent. dir is usually
// what Dir returns.
func Open(dir string) (*Store, error) {
	return OpenWith(dir, Hooks{})
}

// OpenWith is Open with hooks set. See Hooks.
func OpenWith(dir string, h Hooks) (*Store, error) {
	if err := ensureDir(dir); err != nil {
		return nil, err
	}
	return &Store{dir: dir, hooks: h}, nil
}

// StageResult says what one Stage call did. It is populated whenever Stage
// got as far as reading the state, including when it returns an error beside
// it.
type StageResult struct {
	// Batch is the number of the batch Stage wrote, 0 when it wrote none.
	Batch int
	// Staged is how many rows the batch holds.
	Staged int
	// Skipped is how many rows were already recorded in the Ledger, already
	// pending in the Outbox, or repeated a message id earlier in the input.
	Skipped int
	// Deferred is how many new rows did not fit in the Outbox. They are in
	// neither the Ledger nor the Outbox, so a later Stage offers them again.
	Deferred int
	// Pruned is how many Ledger entries were dropped because their transcript
	// no longer exists.
	Pruned int
	// Quarantined names the corrupt files this call moved aside.
	Quarantined []string
}

// Stage records rows, all read from the transcript fileKey, as handed off:
// every row whose message id is neither in the Ledger nor pending in the
// Outbox is written to a new Outbox batch, and the Ledger then records those
// ids. The batch is always written before the Ledger, so a crash between the
// two leaves rows pending but unrecorded, and the next Stage skips them
// because they are pending. Rows found pending but unrecorded are recorded in
// the Ledger by that next Stage.
//
// When nothing is new, no batch is written. When the Outbox cannot take every
// new row within its bound, Stage stages the longest prefix that fits, records
// exactly those, and returns ErrOutboxFull beside a result saying so.
//
// Stage also drops Ledger entries whose transcript no longer exists.
func (s *Store) Stage(fileKey string, rows []usage.Row) (res StageResult, err error) {
	lk, err := acquire(s.dir)
	if err != nil {
		return res, err
	}
	defer func() {
		if rerr := lk.release(); rerr != nil && err == nil {
			err = fmt.Errorf("usagestate: release lock: %w", rerr)
		}
	}()

	led, q, err := loadLedger(s.dir)
	if err != nil {
		return res, err
	}
	if q != "" {
		res.Quarantined = append(res.Quarantined, q)
	}
	ob, qs, err := readOutbox(s.dir, true, false)
	res.Quarantined = append(res.Quarantined, qs...)
	if err != nil {
		return res, err
	}

	res.Pruned = led.pruneMissing()
	changed := res.Pruned > 0

	var keys []Key
	var fresh []usage.Row
	seen := map[string]struct{}{}
	for _, r := range rows {
		k := Key{FileKey: fileKey, MessageID: r.MessageID}
		if _, dup := seen[k.MessageID]; dup {
			res.Skipped++
			continue
		}
		seen[k.MessageID] = struct{}{}
		if led.has(fileKey, k.MessageID) {
			res.Skipped++
			continue
		}
		if _, ok := ob.pending[k]; ok {
			led.add(fileKey, k.MessageID)
			changed = true
			res.Skipped++
			continue
		}
		keys = append(keys, k)
		fresh = append(fresh, r)
	}

	full := false
	if len(fresh) > 0 {
		id, err := newBatchID()
		if err != nil {
			return res, err
		}
		n, raw, err := fitBatch(id, keys, fresh, outboxLimit-ob.bytes)
		if err != nil {
			return res, err
		}
		if n < len(fresh) {
			full = true
			res.Deferred = len(fresh) - n
		}
		if n > 0 {
			number := ob.highest + 1
			if err := writeBatch(s.dir, number, raw); err != nil {
				return res, err
			}
			res.Batch = number
			res.Staged = n
			if s.hooks.AfterBatchWrite != nil {
				if err := s.hooks.AfterBatchWrite(); err != nil {
					return res, fmt.Errorf("usagestate: after batch write: %w", err)
				}
			}
			for _, k := range keys[:n] {
				led.add(fileKey, k.MessageID)
			}
			changed = true
		}
	}

	if changed {
		if err := saveLedger(s.dir, led); err != nil {
			return res, err
		}
	}
	if full {
		return res, ErrOutboxFull
	}
	return res, nil
}

// fitBatch returns how many leading rows a new batch can hold within room
// bytes, and that batch serialised. When even one row does not fit it returns
// 0 and no bytes.
func fitBatch(id string, keys []Key, rows []usage.Row, room int64) (int, []byte, error) {
	raw, err := marshalBatch(id, keys, rows)
	if err != nil {
		return 0, nil, err
	}
	if int64(len(raw)) <= room {
		return len(rows), raw, nil
	}
	// The serialised size grows with the prefix length, so bisection finds
	// the longest prefix that fits. lo always fits; hi+1 never does.
	lo, hi := 0, len(rows)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		b, err := marshalBatch(id, keys[:mid], rows[:mid])
		if err != nil {
			return 0, nil, err
		}
		if int64(len(b)) <= room {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo == 0 {
		return 0, nil, nil
	}
	raw, err = marshalBatch(id, keys[:lo], rows[:lo])
	if err != nil {
		return 0, nil, err
	}
	return lo, raw, nil
}

// Pending returns every batch in the Outbox, oldest first. A corrupt batch is
// moved aside to a quarantine name and left out; Stats lists it.
func (s *Store) Pending() (batches []Batch, err error) {
	lk, err := acquire(s.dir)
	if err != nil {
		return nil, err
	}
	defer func() {
		if rerr := lk.release(); rerr != nil && err == nil {
			err = fmt.Errorf("usagestate: release lock: %w", rerr)
		}
	}()
	ob, _, err := readOutbox(s.dir, true, true)
	if err != nil {
		return nil, err
	}
	return ob.batches, nil
}

// Ack removes b from the Outbox once Ingest has accepted it. Acknowledging a
// batch that is already gone is not an error, and a later batch that reuses
// b's number is never removed in b's place.
func (s *Store) Ack(b Batch) (err error) {
	lk, err := acquire(s.dir)
	if err != nil {
		return err
	}
	defer func() {
		if rerr := lk.release(); rerr != nil && err == nil {
			err = fmt.Errorf("usagestate: release lock: %w", rerr)
		}
	}()
	return removeBatch(s.dir, b)
}

// Stats describes the state directory.
type Stats struct {
	// LedgerFiles is how many transcripts the Ledger has an entry for.
	LedgerFiles int
	// SentIDs is how many message ids the Ledger records across them.
	SentIDs int
	// PendingBatches, PendingRows and PendingBytes describe the Outbox.
	PendingBatches int
	PendingRows    int
	PendingBytes   int64
	// Quarantined names every corrupt file moved aside in the directory, by
	// this call or an earlier one.
	Quarantined []string
}

// Stats reports the size of the Ledger and the Outbox. Like Pending, it moves
// a corrupt Ledger or batch aside, and lists every quarantined file present.
func (s *Store) Stats() (st Stats, err error) {
	lk, err := acquire(s.dir)
	if err != nil {
		return st, err
	}
	defer func() {
		if rerr := lk.release(); rerr != nil && err == nil {
			err = fmt.Errorf("usagestate: release lock: %w", rerr)
		}
	}()
	led, _, err := loadLedger(s.dir)
	if err != nil {
		return st, err
	}
	ob, _, err := readOutbox(s.dir, true, false)
	if err != nil {
		return st, err
	}
	st.LedgerFiles = len(led.files)
	st.SentIDs = led.sentCount()
	st.PendingBatches = len(ob.batches)
	for _, b := range ob.batches {
		st.PendingRows += b.rowCount
	}
	st.PendingBytes = ob.bytes
	st.Quarantined, err = quarantinedFiles(s.dir)
	if err != nil {
		return st, err
	}
	return st, nil
}

// Unsent returns the rows of fileKey that a Stage would stage now: those whose
// message id is neither in the Ledger nor pending in the Outbox, first
// occurrence of each id only, in input order. It writes nothing: a corrupt
// Ledger reads as empty and a corrupt batch is skipped, both left in place for
// the next Stage to move aside. It is a preview; Stage filters again under
// its own lock.
func (s *Store) Unsent(fileKey string, rows []usage.Row) (out []usage.Row, err error) {
	lk, err := acquire(s.dir)
	if err != nil {
		return nil, err
	}
	defer func() {
		if rerr := lk.release(); rerr != nil && err == nil {
			err = fmt.Errorf("usagestate: release lock: %w", rerr)
		}
	}()
	led := peekLedger(s.dir)
	ob, _, err := readOutbox(s.dir, false, false)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for _, r := range rows {
		if _, dup := seen[r.MessageID]; dup {
			continue
		}
		seen[r.MessageID] = struct{}{}
		if led.has(fileKey, r.MessageID) {
			continue
		}
		if _, ok := ob.pending[Key{FileKey: fileKey, MessageID: r.MessageID}]; ok {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// peekLedger reads dir's Ledger without moving a corrupt one aside: a missing,
// unreadable or unparsable Ledger reads as empty.
func peekLedger(dir string) *ledger {
	raw, err := os.ReadFile(filepath.Join(dir, ledgerFileName)) // #nosec G304 -- agtk's own Ledger in its state directory
	if err != nil {
		return newLedger()
	}
	l, err := parseLedger(raw)
	if err != nil {
		return newLedger()
	}
	return l
}
