//go:build unix

package tests

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/usage"
	"github.com/pedromvgomes/agentic-toolkit/internal/usagestate"
)

func TestFlushingTheSameRowsTwiceStagesThemOnce(t *testing.T) {
	s, _, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	ids := idRange(0, 5)

	first := mustStage(t, s, file, rows(ids...))
	if first.Staged != 5 || first.Skipped != 0 || first.Batch == 0 {
		t.Fatalf("first flush = %+v, want 5 staged in a batch", first)
	}
	second := mustStage(t, s, file, rows(ids...))
	if second.Staged != 0 || second.Skipped != 5 || second.Batch != 0 {
		t.Errorf("second flush = %+v, want nothing staged, 5 skipped, no batch", second)
	}

	bs := mustPending(t, s)
	if len(bs) != 1 {
		t.Fatalf("pending batches = %d, want 1", len(bs))
	}
	assertExactlyOnce(t, "pending", pendingIDs(t, bs, file), ids)

	if err := s.Ack(bs[0]); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	third := mustStage(t, s, file, rows(ids...))
	if third.Staged != 0 || third.Batch != 0 {
		t.Errorf("flush after ack = %+v, want nothing staged", third)
	}
	if got := mustPending(t, s); len(got) != 0 {
		t.Errorf("pending after ack and re-flush = %d batches, want none", len(got))
	}
}

func TestRepeatedMessageIDInOneFlushIsStagedOnce(t *testing.T) {
	s, _, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")

	res := mustStage(t, s, file, rows("msg_1", "msg_2", "msg_1"))
	if res.Staged != 2 || res.Skipped != 1 {
		t.Errorf("Stage = %+v, want 2 staged and 1 skipped", res)
	}
	assertExactlyOnce(t, "pending", pendingIDs(t, mustPending(t, s), file), []string{"msg_1", "msg_2"})
}

func TestCrashBetweenBatchAndLedgerWriteLosesAndDuplicatesNothing(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	tdir := t.TempDir()
	file := transcript(t, tdir, "a.jsonl")
	ids := idRange(0, 4)

	crash := errors.New("simulated crash")
	crashing, err := usagestate.OpenWith(stateDir, usagestate.Hooks{AfterBatchWrite: func() error { return crash }})
	if err != nil {
		t.Fatalf("OpenWith: %v", err)
	}
	res, err := crashing.Stage(file, rows(ids...))
	if !errors.Is(err, crash) {
		t.Fatalf("Stage error = %v, want the hook's error", err)
	}
	if res.Batch == 0 || res.Staged != 4 {
		t.Errorf("interrupted Stage = %+v, want a batch of 4 written", res)
	}

	s, err := usagestate.Open(stateDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st := mustStats(t, s)
	if st.SentIDs != 0 || st.PendingRows != 4 {
		t.Fatalf("after the crash: %+v, want 4 rows pending and none in the Ledger", st)
	}
	if unsent, err := s.Unsent(file, rows(ids...)); err != nil || len(unsent) != 0 {
		t.Errorf("Unsent after the crash = %v, %v; want nothing, the rows are pending", unsent, err)
	}

	// The next flush re-derives the same rows plus one new one.
	next := mustStage(t, s, file, rows(append(ids, "msg_new")...))
	if next.Staged != 1 || next.Skipped != 4 {
		t.Errorf("next Stage = %+v, want only the new row staged", next)
	}
	st = mustStats(t, s)
	if st.SentIDs != 5 {
		t.Errorf("Ledger records %d ids, want all 5", st.SentIDs)
	}
	bs := mustPending(t, s)
	assertExactlyOnce(t, "pending", pendingIDs(t, bs, file), append(ids, "msg_new"))

	for _, b := range bs {
		if err := s.Ack(b); err != nil {
			t.Fatalf("Ack: %v", err)
		}
	}
	again := mustStage(t, s, file, rows(append(ids, "msg_new")...))
	if again.Staged != 0 || again.Batch != 0 {
		t.Errorf("Stage after ack = %+v, want nothing re-sent", again)
	}
	if got := mustPending(t, s); len(got) != 0 {
		t.Errorf("pending after ack = %d batches, want none", len(got))
	}
}

func TestUnacknowledgedBatchIsPendingAgainOnTheNextCycle(t *testing.T) {
	s, _, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")

	mustStage(t, s, file, rows("msg_1", "msg_2"))
	first := mustPending(t, s)
	if len(first) != 1 {
		t.Fatalf("pending = %d batches, want 1", len(first))
	}
	// The send fails: no Ack. The next cycle stages more and reads the Outbox again.
	mustStage(t, s, file, rows("msg_1", "msg_2", "msg_3"))
	second := mustPending(t, s)
	if len(second) != 2 {
		t.Fatalf("pending on the next cycle = %d batches, want 2", len(second))
	}
	if !reflect.DeepEqual(second[0], first[0]) {
		t.Errorf("the unsent batch changed between cycles:\n got %+v\nwant %+v", second[0], first[0])
	}
	if second[0].Number >= second[1].Number {
		t.Errorf("batches not oldest first: %d then %d", second[0].Number, second[1].Number)
	}
	assertExactlyOnce(t, "pending", pendingIDs(t, second, file), []string{"msg_1", "msg_2", "msg_3"})

	if err := s.Ack(second[0]); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	third := mustPending(t, s)
	if len(third) != 1 || third[0].ID != second[1].ID {
		t.Errorf("after acking the oldest, pending = %+v, want only the newer batch", third)
	}
}

func TestAckingAnAlreadyAcknowledgedBatchIsHarmless(t *testing.T) {
	s, _, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")

	mustStage(t, s, file, rows("msg_1"))
	b := mustPending(t, s)[0]
	if err := s.Ack(b); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if err := s.Ack(b); err != nil {
		t.Errorf("second Ack: %v, want nil", err)
	}
}

func TestAckLeavesALaterBatchThatReusesTheNumber(t *testing.T) {
	s, _, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")

	mustStage(t, s, file, rows("msg_1"))
	old := mustPending(t, s)[0]
	if err := s.Ack(old); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	mustStage(t, s, file, rows("msg_2"))
	later := mustPending(t, s)
	if len(later) != 1 {
		t.Fatalf("pending = %d batches, want 1", len(later))
	}
	if later[0].Number != old.Number {
		t.Fatalf("the later batch took number %d, want the acknowledged batch's %d", later[0].Number, old.Number)
	}
	if later[0].ID == old.ID {
		t.Fatalf("the later batch reuses the acknowledged batch's id %q", old.ID)
	}

	if err := s.Ack(old); err != nil {
		t.Fatalf("stale Ack: %v", err)
	}
	after := mustPending(t, s)
	if len(after) != 1 || after[0].ID != later[0].ID {
		t.Errorf("a stale Ack removed the later batch: pending = %+v", after)
	}
}

// heldTranscript is a transcript whose last assistant message, msg_2, has no
// stop_reason yet, so usage.ParseFile holds it.
const heldTranscript = `{"type":"assistant","sessionId":"sess-1","version":"2.0.0","timestamp":"2026-10-10T06:42:53.777Z","isSidechain":false,"entrypoint":"cli","cwd":"/work/example","gitBranch":"main","message":{"id":"msg_1","model":"claude-opus-5-5","stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":7,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens_details":{"thinking_tokens":0}}}}
{"type":"assistant","sessionId":"sess-1","version":"2.0.0","timestamp":"2026-10-10T06:43:10.000Z","isSidechain":false,"entrypoint":"cli","cwd":"/work/example","gitBranch":"main","message":{"id":"msg_2","model":"claude-opus-5-5","stop_reason":null,"usage":{"input_tokens":5,"output_tokens":11,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens_details":{"thinking_tokens":0}}}}
`

// heldCompletion is msg_2's final record, carrying its stop_reason.
const heldCompletion = `{"type":"assistant","sessionId":"sess-1","version":"2.0.0","timestamp":"2026-10-10T06:43:11.000Z","isSidechain":false,"entrypoint":"cli","cwd":"/work/example","gitBranch":"main","message":{"id":"msg_2","model":"claude-opus-5-5","stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":13,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens_details":{"thinking_tokens":0}}}}
`

func TestHeldMessageShipsOnlyOnceALaterFlushCompletesIt(t *testing.T) {
	s, _, tdir := newStore(t)
	file := filepath.Join(tdir, "session.jsonl")
	if err := os.WriteFile(file, []byte(heldTranscript), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	parsed, err := usage.ParseFile(file, usage.Options{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(parsed.Held) != 1 || parsed.Held[0].MessageID != "msg_2" {
		t.Fatalf("held = %v, want msg_2", parsed.Held)
	}
	mustStage(t, s, file, parsed.Rows)
	got := pendingIDs(t, mustPending(t, s), file)
	if !reflect.DeepEqual(got, []string{"msg_1"}) {
		t.Fatalf("pending while msg_2 is held = %v, want only msg_1", got)
	}

	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open transcript: %v", err)
	}
	if _, err := f.WriteString(heldCompletion); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	parsed, err = usage.ParseFile(file, usage.Options{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	res := mustStage(t, s, file, parsed.Rows)
	if res.Staged != 1 || res.Skipped != 1 {
		t.Errorf("Stage once msg_2 completes = %+v, want 1 staged and 1 skipped", res)
	}
	bs := mustPending(t, s)
	assertExactlyOnce(t, "pending", pendingIDs(t, bs, file), []string{"msg_1", "msg_2"})
	for _, b := range bs {
		for _, r := range b.Rows {
			if r.MessageID == "msg_2" && r.OutputTokens != 13 {
				t.Errorf("msg_2 output = %d, want the completed record's 13", r.OutputTokens)
			}
		}
	}
}

func TestStateUnderXDGStateHomeLeavesTheWorkingTreeUntouched(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	before := treeListing(t, wd)

	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", home)
	dir, err := usagestate.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if want := filepath.Join(home, "agentic-toolkit", "usage"); dir != want {
		t.Fatalf("Dir = %q, want %q", dir, want)
	}
	s, err := usagestate.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	file := transcript(t, t.TempDir(), "a.jsonl")
	mustStage(t, s, file, rows("msg_1", "msg_2"))
	mustStats(t, s)

	for _, name := range []string{"usage.lock", "ledger.json", "outbox"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s not under XDG_STATE_HOME: %v", name, err)
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat state dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("state dir mode = %o, want 700", perm)
	}
	ledgerInfo, err := os.Stat(filepath.Join(dir, "ledger.json"))
	if err != nil {
		t.Fatalf("stat ledger: %v", err)
	}
	if perm := ledgerInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("ledger mode = %o, want 600", perm)
	}

	after := treeListing(t, wd)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("the working tree changed:\nbefore %v\n after %v", before, after)
	}
}

// treeListing lists every path under root, relative to it.
func treeListing(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(out)
	return out
}

func TestVanishedTranscriptIsPrunedFromTheLedger(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	gone := transcript(t, tdir, "gone.jsonl")
	kept := transcript(t, tdir, "kept.jsonl")
	other := transcript(t, tdir, "other.jsonl")

	mustStage(t, s, gone, rows("msg_1", "msg_2"))
	mustStage(t, s, kept, rows("msg_3"))
	if st := mustStats(t, s); st.LedgerFiles != 2 || st.SentIDs != 3 {
		t.Fatalf("before pruning: %+v, want 2 files and 3 ids", st)
	}

	if err := os.Remove(gone); err != nil {
		t.Fatalf("remove transcript: %v", err)
	}
	res := mustStage(t, s, other, rows("msg_4"))
	if res.Pruned != 1 {
		t.Errorf("Pruned = %d, want 1", res.Pruned)
	}
	st := mustStats(t, s)
	if st.LedgerFiles != 2 || st.SentIDs != 2 {
		t.Errorf("after pruning: %+v, want the kept and the other transcript, 2 ids", st)
	}
	led := ledgerOnDisk(t, stateDir)
	if _, ok := led[gone]; ok {
		t.Errorf("the Ledger still has an entry for %s", gone)
	}
	for _, k := range []string{kept, other} {
		if _, ok := led[k]; !ok {
			t.Errorf("the Ledger lost the entry for %s", k)
		}
	}
	// The pruned transcript's rows are still pending: pruning touches the
	// Ledger, not the Outbox.
	assertExactlyOnce(t, "pending", pendingIDs(t, mustPending(t, s), gone), []string{"msg_1", "msg_2"})
}

func TestStageOverASoundLedgerQuarantinesNothing(t *testing.T) {
	s, _, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")

	mustStage(t, s, file, rows("msg_1"))
	res := mustStage(t, s, file, rows("msg_1", "msg_2"))
	if len(res.Quarantined) != 0 {
		t.Errorf("Stage over a sound Ledger quarantined %v, want nothing", res.Quarantined)
	}
	if st := mustStats(t, s); len(st.Quarantined) != 0 {
		t.Errorf("Stats.Quarantined = %v, want nothing", st.Quarantined)
	}
}

func TestStageWithNothingToRecordWritesNothing(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")

	res := mustStage(t, s, file, nil)
	if res.Batch != 0 || res.Staged != 0 || res.Pruned != 0 {
		t.Errorf("Stage of no rows = %+v, want nothing done", res)
	}
	for _, name := range []string{"ledger.json", "outbox"} {
		if _, err := os.Stat(filepath.Join(stateDir, name)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Stage of no rows created %s: %v", name, err)
		}
	}
}

func TestUnsentLeavesOutRowsTheLedgerRecordsOnceTheirBatchIsAcknowledged(t *testing.T) {
	s, _, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")

	mustStage(t, s, file, rows("msg_1", "msg_2"))
	for _, b := range mustPending(t, s) {
		if err := s.Ack(b); err != nil {
			t.Fatalf("Ack: %v", err)
		}
	}
	unsent, err := s.Unsent(file, rows("msg_1", "msg_2", "msg_3"))
	if err != nil {
		t.Fatalf("Unsent: %v", err)
	}
	if len(unsent) != 1 || unsent[0].MessageID != "msg_3" {
		t.Errorf("Unsent = %v, want only msg_3", unsent)
	}
}

// writeBatchFile writes a well-formed batch document named name into the
// Outbox, holding one row of fileKey with message id.
func writeBatchFile(t *testing.T, stateDir, name, id, fileKey, messageID string) {
	t.Helper()
	doc := map[string]any{
		"version": 1,
		"id":      id,
		"keys":    []usagestate.Key{{FileKey: fileKey, MessageID: messageID}},
		"rows":    []usage.Row{row(messageID)},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal batch: %v", err)
	}
	outbox := filepath.Join(stateDir, "outbox")
	if err := os.MkdirAll(outbox, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outbox, name), raw, 0o600); err != nil {
		t.Fatalf("write batch: %v", err)
	}
}

func TestOutboxFilesWithoutABatchNumberAreIgnoredAndKept(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	mustStage(t, s, file, rows("msg_1"))

	// Well-formed batch documents under names that carry no batch number.
	strays := []string{".json", "notes.txt"}
	for i, name := range strays {
		writeBatchFile(t, stateDir, name, "stray"+strconv.Itoa(i), file, "msg_stray"+strconv.Itoa(i))
	}

	bs := mustPending(t, s)
	if got := pendingIDs(t, bs, file); !reflect.DeepEqual(got, []string{"msg_1"}) {
		t.Errorf("pending beside stray files = %v, want only msg_1", got)
	}
	if st := mustStats(t, s); st.PendingBatches != 1 || len(st.Quarantined) != 0 {
		t.Errorf("Stats = %+v, want 1 batch pending and nothing quarantined", st)
	}
	for _, name := range strays {
		if _, err := os.Stat(filepath.Join(stateDir, "outbox", name)); err != nil {
			t.Errorf("stray file %s was not left in place: %v", name, err)
		}
	}
}

func TestPendingOrdersBatchesByNumberNotByFileName(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	// "10.json" sorts before "2.json" by name.
	writeBatchFile(t, stateDir, "10.json", "ten", file, "msg_10")
	writeBatchFile(t, stateDir, "2.json", "two", file, "msg_2")

	bs := mustPending(t, s)
	var numbers []int
	for _, b := range bs {
		numbers = append(numbers, b.Number)
	}
	if !reflect.DeepEqual(numbers, []int{2, 10}) {
		t.Errorf("pending batch numbers = %v, want [2 10]", numbers)
	}
	if res := mustStage(t, s, file, rows("msg_new")); res.Batch != 11 {
		t.Errorf("new batch number = %d, want 11, one past the highest", res.Batch)
	}
}

func TestStatsListsQuarantinedFilesSortedByPath(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	mustStage(t, s, file, rows("msg_1"))

	// The state directory is listed before its Outbox, and this name sorts
	// after every path under outbox/.
	late := filepath.Join(stateDir, "zzz.corrupt-1")
	if err := os.WriteFile(late, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "outbox", "00000099.json"), []byte("garbage"), 0o600); err != nil {
		t.Fatalf("write batch: %v", err)
	}

	st := mustStats(t, s)
	if len(st.Quarantined) != 2 || !sort.StringsAreSorted(st.Quarantined) {
		t.Errorf("Stats.Quarantined = %v, want 2 paths in sorted order", st.Quarantined)
	}
}
