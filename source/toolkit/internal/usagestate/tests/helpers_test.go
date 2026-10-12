//go:build unix

package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/usage"
	"github.com/pedromvgomes/agentic-toolkit/internal/usagestate"
)

// row is a minimal usage row for message id.
func row(id string) usage.Row {
	return usage.Row{
		Harness:      usage.Harness,
		SessionID:    "sess-1",
		MessageID:    id,
		Model:        "claude-opus-5-5",
		InputTokens:  3,
		OutputTokens: 7,
	}
}

// rows builds a row per id.
func rows(ids ...string) []usage.Row {
	out := make([]usage.Row, 0, len(ids))
	for _, id := range ids {
		out = append(out, row(id))
	}
	return out
}

// idRange returns msg_<from> .. msg_<to-1>.
func idRange(from, to int) []string {
	out := make([]string, 0, to-from)
	for i := from; i < to; i++ {
		out = append(out, fmt.Sprintf("msg_%04d", i))
	}
	return out
}

// transcript creates a real file to key rows by. Stage prunes Ledger entries
// whose transcript does not exist, so every fileKey a test expects to stay in
// the Ledger must be a file on disk.
func transcript(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("create transcript: %v", err)
	}
	return path
}

// newStore opens a Store in a fresh temporary state directory and returns it
// with that directory and a separate directory for transcripts.
func newStore(t *testing.T) (store *usagestate.Store, stateDir, transcripts string) {
	t.Helper()
	stateDir = filepath.Join(t.TempDir(), "state")
	store, err := usagestate.Open(stateDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store, stateDir, t.TempDir()
}

func mustStage(t *testing.T, s *usagestate.Store, fileKey string, rs []usage.Row) usagestate.StageResult {
	t.Helper()
	res, err := s.Stage(fileKey, rs)
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	return res
}

func mustPending(t *testing.T, s *usagestate.Store) []usagestate.Batch {
	t.Helper()
	bs, err := s.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	return bs
}

func mustStats(t *testing.T, s *usagestate.Store) usagestate.Stats {
	t.Helper()
	st, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	return st
}

// pendingIDs lists the message ids of every pending row for fileKey, sorted,
// duplicates kept, and checks each batch's rows match its keys.
func pendingIDs(t *testing.T, bs []usagestate.Batch, fileKey string) []string {
	t.Helper()
	var out []string
	for _, b := range bs {
		if len(b.Keys) != len(b.Rows) {
			t.Fatalf("batch %d: %d keys for %d rows", b.Number, len(b.Keys), len(b.Rows))
		}
		for i, k := range b.Keys {
			if k.MessageID != b.Rows[i].MessageID {
				t.Errorf("batch %d: key %q beside row %q", b.Number, k.MessageID, b.Rows[i].MessageID)
			}
			if k.FileKey == fileKey {
				out = append(out, k.MessageID)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ledgerOnDisk parses ledger.json as the package documents it, failing the
// test when it is not valid JSON.
func ledgerOnDisk(t *testing.T, stateDir string) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(stateDir, "ledger.json"))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	var doc struct {
		Version int `json:"version"`
		Files   map[string]struct {
			Sent []string `json:"sent"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("ledger is not valid JSON: %v\n%s", err, raw)
	}
	if doc.Version != 1 {
		t.Errorf("ledger version = %d, want 1", doc.Version)
	}
	out := map[string][]string{}
	for path, e := range doc.Files {
		ids := append([]string(nil), e.Sent...)
		sort.Strings(ids)
		out[path] = ids
	}
	return out
}

// assertExactlyOnce fails unless got holds every id in want once and nothing
// else.
func assertExactlyOnce(t *testing.T, what string, got, want []string) {
	t.Helper()
	counts := map[string]int{}
	for _, id := range got {
		counts[id]++
	}
	for _, id := range want {
		switch counts[id] {
		case 1:
		case 0:
			t.Errorf("%s: %s lost", what, id)
		default:
			t.Errorf("%s: %s present %d times", what, id, counts[id])
		}
		delete(counts, id)
	}
	for id, n := range counts {
		t.Errorf("%s: unexpected %s (%d times)", what, id, n)
	}
}
