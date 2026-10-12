package usagestate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/usage"
)

const (
	outboxDirName = "outbox"

	// batchSuffix ends every batch file name; the rest of the name is the
	// batch number. Temporary and quarantined files do not end in it, so a
	// directory scan never mistakes them for batches.
	batchSuffix = ".json"

	// batchVersion is the `version` a batch document carries. A document with
	// any other version is corrupt, not guessed at.
	batchVersion = 1

	// outboxLimit bounds the serialised size of every batch in the Outbox
	// together. Past it Stage stages what fits and returns ErrOutboxFull, so
	// an Ingest that stays unreachable cannot fill the disk.
	outboxLimit int64 = 32 << 20
)

// ErrOutboxFull is returned by Stage when the Outbox cannot take every row it
// was given. It comes with a StageResult saying what did land. Test for it
// with errors.Is.
var ErrOutboxFull = errors.New("usagestate: the outbox is full")

// Key identifies one queued row: the transcript it came from and its message
// id.
type Key struct {
	FileKey   string `json:"file"`
	MessageID string `json:"message_id"`
}

// Batch is one file of the Outbox. Rows[i] is the row whose key is Keys[i].
type Batch struct {
	// Number orders batches: a new batch is numbered one past the highest in
	// the Outbox when it is written.
	Number int
	// ID tells this batch apart from a later one written under the same
	// Number after this one was acknowledged. Ack removes only the batch
	// whose ID it is given.
	ID   string
	Keys []Key
	Rows []usage.Row
	// Bytes is the batch file's serialised size.
	Bytes int64
}

// batchDoc is the on-disk form:
//
//	{"version": 1, "id": "<hex>", "keys": [{"file": ..., "message_id": ...}], "rows": [...]}
//
// keys and rows are parallel and must be the same length.
type batchDoc struct {
	Version int         `json:"version"`
	ID      string      `json:"id"`
	Keys    []Key       `json:"keys"`
	Rows    []usage.Row `json:"rows"`
}

func outboxDir(dir string) string {
	return filepath.Join(dir, outboxDirName)
}

func batchFileName(number int) string {
	return fmt.Sprintf("%08d%s", number, batchSuffix)
}

// batchNumber parses a batch file name, reporting false for any name that is
// not one.
func batchNumber(name string) (int, bool) {
	digits, ok := strings.CutSuffix(name, batchSuffix)
	if !ok || digits == "" {
		return 0, false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0, false
	}
	return n, true
}

func newBatchID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("usagestate: batch id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func marshalBatch(id string, keys []Key, rows []usage.Row) ([]byte, error) {
	raw, err := json.Marshal(batchDoc{Version: batchVersion, ID: id, Keys: keys, Rows: rows})
	if err != nil {
		return nil, fmt.Errorf("usagestate: marshal batch: %w", err)
	}
	return append(raw, '\n'), nil
}

func parseBatch(number int, raw []byte) (Batch, error) {
	var doc batchDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Batch{}, err
	}
	if doc.Version != batchVersion {
		return Batch{}, fmt.Errorf("version %d, want %d", doc.Version, batchVersion)
	}
	if doc.ID == "" {
		return Batch{}, errors.New("no batch id")
	}
	if len(doc.Keys) != len(doc.Rows) {
		return Batch{}, fmt.Errorf("%d keys for %d rows", len(doc.Keys), len(doc.Rows))
	}
	return Batch{Number: number, ID: doc.ID, Keys: doc.Keys, Rows: doc.Rows, Bytes: int64(len(raw))}, nil
}

// outbox is the Outbox as one scan of its directory found it.
type outbox struct {
	batches []Batch // ordered by Number
	pending map[Key]struct{}
	bytes   int64
	highest int // highest batch number seen, 0 when there is none
}

// readOutbox scans dir's Outbox. A missing directory is an empty Outbox. When
// mend is true, a batch file that cannot be read or parsed is moved aside to a
// quarantine name and returned in quarantined; when it is false the file is
// left in place and skipped, so the scan writes nothing. Either way a corrupt
// batch never stops the scan. A skipped or quarantined batch still counts
// towards highest, so a new batch is never written over it.
func readOutbox(dir string, mend bool) (ob *outbox, quarantined []string, err error) {
	ob = &outbox{pending: map[Key]struct{}{}}
	odir := outboxDir(dir)
	entries, err := os.ReadDir(odir)
	if errors.Is(err, fs.ErrNotExist) {
		return ob, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("usagestate: read %s: %w", odir, err)
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		n, ok := batchNumber(e.Name())
		if !ok {
			continue
		}
		if n > ob.highest {
			ob.highest = n
		}
		path := filepath.Join(odir, e.Name())
		raw, rerr := os.ReadFile(path) // #nosec G304 -- agtk's own batch file in its state directory
		if errors.Is(rerr, fs.ErrNotExist) {
			continue
		}
		var b Batch
		if rerr == nil {
			b, rerr = parseBatch(n, raw)
		}
		if rerr != nil {
			if !mend {
				continue
			}
			q, qerr := quarantine(path)
			if qerr != nil {
				return nil, nil, qerr
			}
			quarantined = append(quarantined, q)
			continue
		}
		ob.batches = append(ob.batches, b)
		ob.bytes += b.Bytes
		for _, k := range b.Keys {
			ob.pending[k] = struct{}{}
		}
	}
	sort.Slice(ob.batches, func(i, j int) bool { return ob.batches[i].Number < ob.batches[j].Number })
	return ob, quarantined, nil
}

// writeBatch writes raw, a serialised batch, into dir's Outbox as batch
// number, creating the Outbox when absent.
func writeBatch(dir string, number int, raw []byte) error {
	odir := outboxDir(dir)
	if err := ensureDir(odir); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(odir, batchFileName(number)), raw)
}

// removeBatch deletes b's file from dir's Outbox when the file there is still
// b. A file already gone, or one holding a later batch under the same number,
// is left alone and is not an error. A file that no longer parses is left for
// the next scan to quarantine.
func removeBatch(dir string, b Batch) error {
	path := filepath.Join(outboxDir(dir), batchFileName(b.Number))
	raw, err := os.ReadFile(path) // #nosec G304 -- agtk's own batch file in its state directory
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("usagestate: read %s: %w", path, err)
	}
	on, perr := parseBatch(b.Number, raw)
	if perr != nil || on.ID != b.ID {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("usagestate: remove %s: %w", path, err)
	}
	return nil
}

// quarantinedFiles lists every quarantined file in dir and its Outbox.
func quarantinedFiles(dir string) ([]string, error) {
	var out []string
	for _, d := range []string{dir, outboxDir(dir)} {
		entries, err := os.ReadDir(d)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("usagestate: read %s: %w", d, err)
		}
		for _, e := range entries {
			if strings.Contains(e.Name(), ".corrupt-") {
				out = append(out, filepath.Join(d, e.Name()))
			}
		}
	}
	sort.Strings(out)
	return out, nil
}
