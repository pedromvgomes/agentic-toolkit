package usagestate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

const (
	ledgerFileName = "ledger.json"

	// ledgerVersion is the `version` the Ledger document carries. A document
	// with any other version is unreadable, not guessed at.
	ledgerVersion = 1
)

// ledger is the in-memory Ledger: for each transcript path, the set of message
// ids already recorded.
type ledger struct {
	files map[string]map[string]struct{}
}

func newLedger() *ledger {
	return &ledger{files: map[string]map[string]struct{}{}}
}

// has reports whether messageID of fileKey is recorded.
func (l *ledger) has(fileKey, messageID string) bool {
	_, ok := l.files[fileKey][messageID]
	return ok
}

// add records messageID for fileKey.
func (l *ledger) add(fileKey, messageID string) {
	ids := l.files[fileKey]
	if ids == nil {
		ids = map[string]struct{}{}
		l.files[fileKey] = ids
	}
	ids[messageID] = struct{}{}
}

// sentCount is the number of message ids recorded across every file.
func (l *ledger) sentCount() int {
	n := 0
	for _, ids := range l.files {
		n += len(ids)
	}
	return n
}

// pruneMissing drops each entry whose transcript path no longer exists and
// returns how many it dropped. A path that cannot be examined for any reason
// other than not existing is kept.
func (l *ledger) pruneMissing() int {
	dropped := 0
	for path := range l.files {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			delete(l.files, path)
			dropped++
		}
	}
	return dropped
}

// ledgerDoc is the on-disk form:
//
//	{
//	  "version": 1,
//	  "files": {"<transcript path>": {"sent": ["<message id>", ...]}}
//	}
//
// Ids are written sorted and the map marshals with sorted keys, so the same
// Ledger always produces the same bytes.
type ledgerDoc struct {
	Version int                       `json:"version"`
	Files   map[string]ledgerEntryDoc `json:"files"`
}

type ledgerEntryDoc struct {
	Sent []string `json:"sent"`
}

func (l *ledger) marshal() ([]byte, error) {
	doc := ledgerDoc{Version: ledgerVersion, Files: make(map[string]ledgerEntryDoc, len(l.files))}
	for path, ids := range l.files {
		sent := make([]string, 0, len(ids))
		for id := range ids {
			sent = append(sent, id)
		}
		sort.Strings(sent)
		doc.Files[path] = ledgerEntryDoc{Sent: sent}
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func parseLedger(raw []byte) (*ledger, error) {
	var doc ledgerDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc.Version != ledgerVersion {
		return nil, fmt.Errorf("version %d, want %d", doc.Version, ledgerVersion)
	}
	l := newLedger()
	for path, entry := range doc.Files {
		for _, id := range entry.Sent {
			l.add(path, id)
		}
	}
	return l, nil
}

// loadLedger reads dir's Ledger. A missing file is an empty Ledger. A file that
// cannot be read or parsed, or carries another version, is moved aside to a
// quarantine name, returned as quarantined, and the Ledger is empty: the rows it
// vouched for are re-derived from the transcripts and Ingest de-duplicates
// them. An error means the file could not even be moved aside.
func loadLedger(dir string) (l *ledger, quarantined string, err error) {
	path := filepath.Join(dir, ledgerFileName)
	raw, err := os.ReadFile(path) // #nosec G304 -- agtk's own Ledger in its state directory
	if errors.Is(err, fs.ErrNotExist) {
		return newLedger(), "", nil
	}
	if err == nil {
		if l, perr := parseLedger(raw); perr == nil {
			return l, "", nil
		}
	}
	q, qerr := quarantine(path)
	if qerr != nil {
		return nil, "", qerr
	}
	return newLedger(), q, nil
}

// saveLedger writes l to dir's Ledger file atomically.
func saveLedger(dir string, l *ledger) error {
	raw, err := l.marshal()
	if err != nil {
		return fmt.Errorf("usagestate: marshal ledger: %w", err)
	}
	return writeFileAtomic(filepath.Join(dir, ledgerFileName), raw)
}
