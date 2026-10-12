//go:build unix

package tests

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/usage"
	"github.com/pedromvgomes/agentic-toolkit/internal/usagestate"
)

func TestCorruptLedgerIsQuarantinedAndStagingContinues(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	ledgerPath := filepath.Join(stateDir, "ledger.json")
	if err := os.WriteFile(ledgerPath, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write ledger: %v", err)
	}

	res := mustStage(t, s, file, rows("msg_1"))
	if res.Staged != 1 {
		t.Errorf("Stage over a corrupt Ledger = %+v, want 1 staged", res)
	}
	if len(res.Quarantined) != 1 || !strings.HasPrefix(res.Quarantined[0], ledgerPath+".corrupt-") {
		t.Fatalf("Quarantined = %v, want the Ledger moved aside", res.Quarantined)
	}
	raw, err := os.ReadFile(res.Quarantined[0])
	if err != nil || string(raw) != "{not json" {
		t.Errorf("quarantined Ledger = %q, %v; want the corrupt bytes kept", raw, err)
	}
	if got := ledgerOnDisk(t, stateDir)[file]; !reflect.DeepEqual(got, []string{"msg_1"}) {
		t.Errorf("fresh Ledger records %v, want [msg_1]", got)
	}
	if st := mustStats(t, s); !reflect.DeepEqual(st.Quarantined, res.Quarantined) {
		t.Errorf("Stats.Quarantined = %v, want %v", st.Quarantined, res.Quarantined)
	}
}

func TestLedgerOfAnotherVersionIsQuarantined(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	if err := os.WriteFile(filepath.Join(stateDir, "ledger.json"), []byte(`{"version": 2, "files": {}}`), 0o600); err != nil {
		t.Fatalf("write ledger: %v", err)
	}
	res := mustStage(t, s, file, rows("msg_1"))
	if len(res.Quarantined) != 1 {
		t.Errorf("Quarantined = %v, want the Ledger moved aside", res.Quarantined)
	}
}

func TestCorruptBatchIsQuarantinedAndPendingContinues(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	mustStage(t, s, file, rows("msg_1"))

	outbox := filepath.Join(stateDir, "outbox")
	corrupt := filepath.Join(outbox, "00000099.json")
	if err := os.WriteFile(corrupt, []byte("garbage"), 0o600); err != nil {
		t.Fatalf("write batch: %v", err)
	}

	bs := mustPending(t, s)
	if len(bs) != 1 || !reflect.DeepEqual(pendingIDs(t, bs, file), []string{"msg_1"}) {
		t.Errorf("Pending beside a corrupt batch = %+v, want only the sound batch", bs)
	}
	if _, err := os.Stat(corrupt); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the corrupt batch is still under its batch name: %v", err)
	}
	st := mustStats(t, s)
	if len(st.Quarantined) != 1 || !strings.HasPrefix(st.Quarantined[0], corrupt+".corrupt-") {
		t.Fatalf("Stats.Quarantined = %v, want the corrupt batch", st.Quarantined)
	}
	if raw, err := os.ReadFile(st.Quarantined[0]); err != nil || string(raw) != "garbage" {
		t.Errorf("quarantined batch = %q, %v; want the corrupt bytes kept", raw, err)
	}
	if st.PendingBatches != 1 || st.PendingRows != 1 {
		t.Errorf("Stats = %+v, want 1 batch of 1 row pending", st)
	}

	res := mustStage(t, s, file, rows("msg_1", "msg_2"))
	if res.Staged != 1 {
		t.Errorf("Stage after the quarantine = %+v, want only msg_2 staged", res)
	}
}

func TestCorruptBatchFoundByStageIsQuarantinedAndReported(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	outbox := filepath.Join(stateDir, "outbox")
	if err := os.MkdirAll(outbox, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	corrupt := filepath.Join(outbox, "00000001.json")
	// Keys and rows of different lengths make a batch unreadable even though
	// it is valid JSON.
	if err := os.WriteFile(corrupt, []byte(`{"version":1,"id":"ab","keys":[{"file":"x","message_id":"m"}],"rows":[]}`), 0o600); err != nil {
		t.Fatalf("write batch: %v", err)
	}

	res := mustStage(t, s, file, rows("msg_1"))
	if res.Staged != 1 {
		t.Errorf("Stage beside a corrupt batch = %+v, want 1 staged", res)
	}
	if len(res.Quarantined) != 1 || !strings.HasPrefix(res.Quarantined[0], corrupt+".corrupt-") {
		t.Errorf("Quarantined = %v, want the corrupt batch", res.Quarantined)
	}
	if got := pendingIDs(t, mustPending(t, s), file); !reflect.DeepEqual(got, []string{"msg_1"}) {
		t.Errorf("pending = %v, want [msg_1]", got)
	}
}

// bulkyRows returns n rows each carrying size bytes in a string field, so a
// handful of rows fill the Outbox's bound.
func bulkyRows(n, size int) []usage.Row {
	pad := strings.Repeat("x", size)
	out := rows(idRange(0, n)...)
	for i := range out {
		out[i].Cwd = pad
	}
	return out
}

func TestFullOutboxStagesThePrefixThatFitsAndRecordsOnlyThose(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")

	// The Outbox holds 32 MiB of batches: two 11 MiB rows fit with their
	// overhead, a third does not.
	const rowSize = 11 << 20
	in := bulkyRows(4, rowSize)
	ids := idRange(0, 4)

	res, err := s.Stage(file, in)
	if !errors.Is(err, usagestate.ErrOutboxFull) {
		t.Fatalf("Stage err = %v, want ErrOutboxFull", err)
	}
	if res.Staged != 2 || res.Deferred != 2 || res.Batch == 0 {
		t.Fatalf("Stage = %+v, want 2 staged in a batch and 2 deferred", res)
	}
	assertExactlyOnce(t, "ledger", ledgerOnDisk(t, stateDir)[file], ids[:2])
	assertExactlyOnce(t, "pending", pendingIDs(t, mustPending(t, s), file), ids[:2])

	unsent, err := s.Unsent(file, in)
	if err != nil {
		t.Fatalf("Unsent: %v", err)
	}
	var unsentIDs []string
	for _, r := range unsent {
		unsentIDs = append(unsentIDs, r.MessageID)
	}
	assertExactlyOnce(t, "unsent", unsentIDs, ids[2:])

	// With the Outbox still full, a retry stages nothing and records nothing.
	res, err = s.Stage(file, in)
	if !errors.Is(err, usagestate.ErrOutboxFull) {
		t.Fatalf("retry err = %v, want ErrOutboxFull", err)
	}
	if res.Staged != 0 || res.Batch != 0 || res.Deferred != 2 || res.Skipped != 2 {
		t.Errorf("retry = %+v, want 2 skipped, 2 deferred, no batch", res)
	}
	assertExactlyOnce(t, "ledger", ledgerOnDisk(t, stateDir)[file], ids[:2])

	// Once Ingest accepts the pending batch, the deferred rows land.
	for _, b := range mustPending(t, s) {
		if err := s.Ack(b); err != nil {
			t.Fatalf("Ack: %v", err)
		}
	}
	res = mustStage(t, s, file, in)
	if res.Staged != 2 || res.Deferred != 0 {
		t.Errorf("Stage after ack = %+v, want the 2 deferred rows staged", res)
	}
	assertExactlyOnce(t, "ledger", ledgerOnDisk(t, stateDir)[file], ids)
	assertExactlyOnce(t, "pending", pendingIDs(t, mustPending(t, s), file), ids[2:])
}

// outboxLimit mirrors the bound the package documents for the Outbox.
const outboxLimit = 32 << 20

// rowFillingTheOutbox returns a row with message id whose one-row batch
// serialises to exactly the Outbox's bound. It measures the batch overhead by
// staging and acknowledging a probe row with an id of the same length, so s's
// Outbox is empty again when it returns.
func rowFillingTheOutbox(t *testing.T, s *usagestate.Store, file, probeID, id string) usage.Row {
	t.Helper()
	if len(probeID) != len(id) {
		t.Fatalf("probe id %q and id %q differ in length", probeID, id)
	}
	probe := row(probeID)
	probe.Cwd = "x"
	mustStage(t, s, file, []usage.Row{probe})
	bs := mustPending(t, s)
	if len(bs) != 1 {
		t.Fatalf("pending after the probe = %d batches, want 1", len(bs))
	}
	overhead := int(bs[0].Bytes) - len(probe.Cwd)
	if err := s.Ack(bs[0]); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	r := row(id)
	r.Cwd = strings.Repeat("x", outboxLimit-overhead)
	return r
}

func TestBatchOfExactlyTheOutboxBoundIsStaged(t *testing.T) {
	s, _, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	r := rowFillingTheOutbox(t, s, file, "msg_0000", "msg_0001")

	res, err := s.Stage(file, []usage.Row{r})
	if err != nil {
		t.Fatalf("Stage of a batch exactly at the bound: %v", err)
	}
	if res.Staged != 1 || res.Deferred != 0 {
		t.Errorf("Stage = %+v, want the row staged", res)
	}
	bs := mustPending(t, s)
	if len(bs) != 1 || bs[0].Bytes != outboxLimit {
		t.Errorf("pending = %d batches, want 1 of %d bytes", len(bs), outboxLimit)
	}
}

func TestPrefixOfExactlyTheOutboxBoundIsStagedAndTheRestDeferred(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	r := rowFillingTheOutbox(t, s, file, "msg_0000", "msg_0001")

	res, err := s.Stage(file, []usage.Row{r, row("msg_0002")})
	if !errors.Is(err, usagestate.ErrOutboxFull) {
		t.Fatalf("Stage err = %v, want ErrOutboxFull", err)
	}
	if res.Staged != 1 || res.Deferred != 1 {
		t.Errorf("Stage = %+v, want the first row staged and the second deferred", res)
	}
	got := ledgerOnDisk(t, stateDir)[file]
	if !reflect.DeepEqual(got, []string{"msg_0000", "msg_0001"}) {
		t.Errorf("Ledger = %v, want the probe and the staged row", got)
	}
}

func TestStatsReportsAStateDirectoryItCannotList(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode")
	}
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	mustStage(t, s, file, rows("msg_1"))

	// Search permission without read: every file in the directory opens, but
	// the directory itself cannot be listed.
	if err := os.Chmod(stateDir, 0o300); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(stateDir, 0o700) })

	if _, err := s.Stats(); err == nil {
		t.Error("Stats over an unlistable state directory returned no error")
	}
}
