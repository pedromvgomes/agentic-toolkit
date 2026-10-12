//go:build unix

package tests

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/usage"
	"github.com/pedromvgomes/agentic-toolkit/internal/usagestate"
)

// writeFile writes content to path, failing the test when it cannot.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// assertFileHolds fails unless path is a regular file holding exactly want.
func assertFileHolds(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != want {
		t.Errorf("%s = %q, %v; want %q left untouched", path, raw, err, want)
	}
}

// assertAbsent fails unless nothing exists at path.
func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s exists (%v), want nothing written there", path, err)
	}
}

// assertNoLeftovers fails when dir holds a temporary or quarantined file.
func assertNoLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") || strings.Contains(e.Name(), ".corrupt-") {
			t.Errorf("%s left behind in %s", e.Name(), dir)
		}
	}
}

// writeBatchDoc writes doc, serialised, as the Outbox file name.
func writeBatchDoc(t *testing.T, stateDir, name string, doc map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal batch: %v", err)
	}
	path := filepath.Join(stateDir, "outbox", name)
	writeFile(t, path, string(raw))
	return path
}

func TestDirFallsBackToTheHomeDirectoryWithoutXDGStateHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", home)

	got, err := usagestate.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if want := filepath.Join(home, ".local", "state", "agentic-toolkit", "usage"); got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
	assertAbsent(t, filepath.Join(home, ".local"))
}

func TestDirFailsWhenNeitherXDGStateHomeNorHomeIsSet(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")

	if got, err := usagestate.Dir(); err == nil {
		t.Errorf("Dir = %q, want an error locating the home directory", got)
	}
}

func TestOpenFailsWhenTheStateDirectoryCannotBeADirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	writeFile(t, file, "keep")

	for _, dir := range []string{file, filepath.Join(file, "state")} {
		if _, err := usagestate.Open(dir); err == nil {
			t.Errorf("Open(%s) under a regular file returned no error", dir)
		}
	}
	assertFileHolds(t, file, "keep")
}

func TestEveryOperationFailsWhenTheStateDirectoryIsReplacedByAFile(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	if err := os.RemoveAll(stateDir); err != nil {
		t.Fatalf("remove: %v", err)
	}
	writeFile(t, stateDir, "keep")

	if _, err := s.Stage(file, rows("msg_1")); err == nil {
		t.Error("Stage returned no error")
	}
	if _, err := s.Pending(); err == nil {
		t.Error("Pending returned no error")
	}
	if err := s.Ack(usagestate.Batch{Number: 1, ID: "x"}); err == nil {
		t.Error("Ack returned no error")
	}
	if _, err := s.Stats(); err == nil {
		t.Error("Stats returned no error")
	}
	if _, err := s.Unsent(file, rows("msg_1")); err == nil {
		t.Error("Unsent returned no error")
	}
	assertFileHolds(t, stateDir, "keep")
}

func TestStageFailsWithoutWaitingWhenTheLockFileIsADirectory(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	if err := os.Mkdir(filepath.Join(stateDir, "usage.lock"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err := s.Stage(file, rows("msg_1"))
	if err == nil || errors.Is(err, usagestate.ErrBusy) {
		t.Fatalf("Stage error = %v, want a failure to open the lock, not ErrBusy", err)
	}
	assertAbsent(t, filepath.Join(stateDir, "ledger.json"))
	assertAbsent(t, filepath.Join(stateDir, "outbox"))
}

func TestOperationsReadingTheOutboxFailWhenItIsARegularFile(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	outbox := filepath.Join(stateDir, "outbox")
	writeFile(t, outbox, "keep")

	if res, err := s.Stage(file, rows("msg_1")); err == nil || res.Batch != 0 {
		t.Errorf("Stage = %+v, %v; want an error and no batch", res, err)
	}
	if _, err := s.Pending(); err == nil {
		t.Error("Pending returned no error")
	}
	if _, err := s.Stats(); err == nil {
		t.Error("Stats returned no error")
	}
	if _, err := s.Unsent(file, rows("msg_1")); err == nil {
		t.Error("Unsent returned no error")
	}
	assertFileHolds(t, outbox, "keep")
	assertAbsent(t, filepath.Join(stateDir, "ledger.json"))
}

func TestStageRecordsNothingWhenTheOutboxCannotBeCreated(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	// A dangling symlink lists as an empty Outbox but blocks creating one.
	outbox := filepath.Join(stateDir, "outbox")
	if err := os.Symlink(filepath.Join(stateDir, "nowhere"), outbox); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if bs := mustPending(t, s); len(bs) != 0 {
		t.Errorf("Pending = %+v, want an empty Outbox", bs)
	}
	res, err := s.Stage(file, rows("msg_1"))
	if err == nil || res.Batch != 0 || res.Staged != 0 {
		t.Fatalf("Stage = %+v, %v; want an error and nothing staged", res, err)
	}
	assertAbsent(t, filepath.Join(stateDir, "ledger.json"))
	assertAbsent(t, filepath.Join(stateDir, "nowhere"))
	assertNoLeftovers(t, stateDir)
}

func TestStageThatCannotReplaceTheLedgerKeepsTheBatchPendingAndLeavesNoTempFile(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	file := transcript(t, t.TempDir(), "a.jsonl")
	ledgerPath := filepath.Join(stateDir, "ledger.json")
	// A non-empty directory under the Ledger's name refuses the rename.
	block := func() error { return os.MkdirAll(filepath.Join(ledgerPath, "x"), 0o700) }
	blocked, err := usagestate.OpenWith(stateDir, usagestate.Hooks{AfterBatchWrite: block})
	if err != nil {
		t.Fatalf("OpenWith: %v", err)
	}

	res, err := blocked.Stage(file, rows("msg_1"))
	if err == nil {
		t.Fatal("Stage over an unreplaceable Ledger returned no error")
	}
	if res.Batch == 0 || res.Staged != 1 {
		t.Errorf("Stage = %+v, want the batch written before the Ledger failed", res)
	}
	assertNoLeftovers(t, stateDir)
	if fi, err := os.Stat(ledgerPath); err != nil || !fi.IsDir() {
		t.Errorf("the directory under the Ledger's name was disturbed: %v", err)
	}

	if err := os.RemoveAll(ledgerPath); err != nil {
		t.Fatalf("remove: %v", err)
	}
	s, err := usagestate.Open(stateDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	next := mustStage(t, s, file, rows("msg_1"))
	if next.Staged != 0 || next.Skipped != 1 {
		t.Errorf("next Stage = %+v, want the pending row skipped", next)
	}
	assertExactlyOnce(t, "pending", pendingIDs(t, mustPending(t, s), file), []string{"msg_1"})
	if got := ledgerOnDisk(t, stateDir)[file]; !reflect.DeepEqual(got, []string{"msg_1"}) {
		t.Errorf("Ledger = %v, want [msg_1] recorded by the next Stage", got)
	}
}

func TestStageReportsAStateDirectoryThatVanishesBeforeTheLedgerWrite(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	moved := filepath.Join(root, "moved")
	file := transcript(t, t.TempDir(), "a.jsonl")
	vanish := func() error { return os.Rename(stateDir, moved) }
	s, err := usagestate.OpenWith(stateDir, usagestate.Hooks{AfterBatchWrite: vanish})
	if err != nil {
		t.Fatalf("OpenWith: %v", err)
	}

	if _, err := s.Stage(file, rows("msg_1")); err == nil {
		t.Fatal("Stage into a vanished state directory returned no error")
	}
	assertAbsent(t, stateDir)
	assertAbsent(t, filepath.Join(moved, "ledger.json"))
	assertNoLeftovers(t, moved)
	if entries, err := os.ReadDir(filepath.Join(moved, "outbox")); err != nil || len(entries) != 1 {
		t.Errorf("outbox = %v, %v; want the one batch written before the failure", entries, err)
	}
}

func TestStageRejectsARowThatCannotBeSerialisedAndWritesNothing(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	bad := row("msg_bad")
	// encoding/json refuses a time outside years 0-9999.
	bad.Timestamp = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)

	res, err := s.Stage(file, []usage.Row{row("msg_1"), bad})
	if err == nil || res.Batch != 0 || res.Staged != 0 {
		t.Fatalf("Stage = %+v, %v; want an error and nothing staged", res, err)
	}
	assertAbsent(t, filepath.Join(stateDir, "outbox"))
	assertAbsent(t, filepath.Join(stateDir, "ledger.json"))
}

func TestDirectoryNamedLikeABatchIsIgnoredByPendingAndRefusedByAck(t *testing.T) {
	s, stateDir, _ := newStore(t)
	dir := filepath.Join(stateDir, "outbox", "00000005.json")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if bs := mustPending(t, s); len(bs) != 0 {
		t.Errorf("Pending = %+v, want the directory left out", bs)
	}
	if err := s.Ack(usagestate.Batch{Number: 5, ID: "x"}); err == nil {
		t.Error("Ack of a batch whose file is a directory returned no error")
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Errorf("the directory was disturbed: %v", err)
	}
	if st := mustStats(t, s); len(st.Quarantined) != 0 {
		t.Errorf("Stats.Quarantined = %v, want nothing moved aside", st.Quarantined)
	}
}

func TestBatchFileNamesThatAreNotBatchNumbersAreIgnoredAndKept(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	mustStage(t, s, file, rows("msg_1"))

	// A non-digit, and a number past the range of an int.
	strays := []string{"12a.json", "99999999999999999999999.json"}
	for i, name := range strays {
		writeBatchFile(t, stateDir, name, "stray"+name, file, "msg_stray"+string(rune('a'+i)))
	}

	if got := pendingIDs(t, mustPending(t, s), file); !reflect.DeepEqual(got, []string{"msg_1"}) {
		t.Errorf("pending beside stray files = %v, want only msg_1", got)
	}
	if res := mustStage(t, s, file, rows("msg_2")); res.Batch != 2 {
		t.Errorf("new batch number = %d, want 2: stray names carry no number", res.Batch)
	}
	if st := mustStats(t, s); len(st.Quarantined) != 0 {
		t.Errorf("Stats.Quarantined = %v, want nothing moved aside", st.Quarantined)
	}
	for _, name := range strays {
		if _, err := os.Stat(filepath.Join(stateDir, "outbox", name)); err != nil {
			t.Errorf("stray file %s was not left in place: %v", name, err)
		}
	}
}

func TestMalformedBatchIsQuarantinedByPendingAndStats(t *testing.T) {
	malformed := map[string]map[string]any{
		"another version": {
			"version": 2,
			"id":      "abc",
			"keys":    []usagestate.Key{{FileKey: "f", MessageID: "msg_1"}},
			"rows":    []usage.Row{row("msg_1")},
		},
		"no id": {
			"version": 1,
			"keys":    []usagestate.Key{{FileKey: "f", MessageID: "msg_1"}},
			"rows":    []usage.Row{row("msg_1")},
		},
		"more keys than rows": {
			"version": 1,
			"id":      "abc",
			"keys":    []usagestate.Key{{FileKey: "f", MessageID: "msg_1"}, {FileKey: "f", MessageID: "msg_2"}},
			"rows":    []usage.Row{row("msg_1")},
		},
	}
	reads := map[string]func(*usagestate.Store) (int, error){
		"Pending": func(s *usagestate.Store) (int, error) {
			bs, err := s.Pending()
			return len(bs), err
		},
		"Stats": func(s *usagestate.Store) (int, error) {
			st, err := s.Stats()
			return st.PendingBatches, err
		},
	}
	for docName, doc := range malformed {
		for readName, read := range reads {
			t.Run(docName+"/"+readName, func(t *testing.T) {
				s, stateDir, _ := newStore(t)
				path := writeBatchDoc(t, stateDir, "00000001.json", doc)

				n, err := read(s)
				if err != nil || n != 0 {
					t.Fatalf("%s = %d batches, %v; want none and no error", readName, n, err)
				}
				assertAbsent(t, path)
				st := mustStats(t, s)
				if len(st.Quarantined) != 1 || !strings.HasPrefix(st.Quarantined[0], path+".corrupt-") {
					t.Errorf("Stats.Quarantined = %v, want the malformed batch", st.Quarantined)
				}
			})
		}
	}
}

func TestUnsentSkipsACorruptBatchAndLeavesItInPlace(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	mustStage(t, s, file, rows("msg_1"))
	corrupt := filepath.Join(stateDir, "outbox", "00000007.json")
	writeFile(t, corrupt, "garbage")

	unsent, err := s.Unsent(file, rows("msg_1", "msg_2"))
	if err != nil {
		t.Fatalf("Unsent: %v", err)
	}
	if len(unsent) != 1 || unsent[0].MessageID != "msg_2" {
		t.Errorf("Unsent = %v, want only msg_2", unsent)
	}
	assertFileHolds(t, corrupt, "garbage")
	assertNoLeftovers(t, filepath.Join(stateDir, "outbox"))
}

func TestUnsentReadsACorruptLedgerAsEmptyAndLeavesItInPlace(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	mustStage(t, s, file, rows("msg_1"))
	for _, b := range mustPending(t, s) {
		if err := s.Ack(b); err != nil {
			t.Fatalf("Ack: %v", err)
		}
	}
	ledgerPath := filepath.Join(stateDir, "ledger.json")
	writeFile(t, ledgerPath, "{not json")

	unsent, err := s.Unsent(file, rows("msg_1", "msg_1", "msg_2"))
	if err != nil {
		t.Fatalf("Unsent: %v", err)
	}
	var ids []string
	for _, r := range unsent {
		ids = append(ids, r.MessageID)
	}
	if !reflect.DeepEqual(ids, []string{"msg_1", "msg_2"}) {
		t.Errorf("Unsent = %v, want [msg_1 msg_2]: the Ledger reads as empty and a repeat is dropped", ids)
	}
	assertFileHolds(t, ledgerPath, "{not json")
	assertNoLeftovers(t, stateDir)
}

func TestStatsOfAFreshStateDirectoryIsEmpty(t *testing.T) {
	s, _, _ := newStore(t)
	if st := mustStats(t, s); !reflect.DeepEqual(st, usagestate.Stats{}) {
		t.Errorf("Stats = %+v, want all zero", st)
	}
}

// pathMax is the longest path, terminating NUL included, the kernel accepts,
// or 0 when this platform's limit is not known here.
func pathMax() int {
	switch runtime.GOOS {
	case "linux":
		return 4096
	case "darwin", "freebsd", "netbsd", "openbsd", "dragonfly":
		return 1024
	}
	return 0
}

// dirForFileAtPathMax returns a directory, created, in which a file named name
// has a path the kernel accepts and the same path plus a quarantine suffix
// does not. It skips the test when the platform's limit is unknown.
func dirForFileAtPathMax(t *testing.T, name string) string {
	t.Helper()
	limit := pathMax()
	if limit == 0 {
		t.Skipf("path length limit unknown on %s", runtime.GOOS)
	}
	// Leaves room for the NUL and a few bytes of margin; the quarantine
	// suffix ".corrupt-<unix nanos>" is far longer than that margin.
	target := limit - 8 - len(string(filepath.Separator)+name)
	dir := t.TempDir()
	for remaining := target - len(dir); remaining > 0; {
		n := min(200, remaining-1)
		if remaining-(n+1) == 1 {
			n--
		}
		dir += string(filepath.Separator) + strings.Repeat("d", n)
		remaining -= n + 1
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Skipf("cannot build a path near the limit: %v", err)
	}
	return dir
}

func TestALedgerThatCannotBeMovedAsideFailsStageAndStatsAndStaysPut(t *testing.T) {
	stateDir := dirForFileAtPathMax(t, "ledger.json")
	s, err := usagestate.Open(stateDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ledgerPath := filepath.Join(stateDir, "ledger.json")
	writeFile(t, ledgerPath, "{not json")
	file := transcript(t, t.TempDir(), "a.jsonl")

	if res, err := s.Stage(file, rows("msg_1")); err == nil || res.Batch != 0 || len(res.Quarantined) != 0 {
		t.Errorf("Stage = %+v, %v; want an error, no batch and nothing quarantined", res, err)
	}
	if _, err := s.Stats(); err == nil {
		t.Error("Stats returned no error")
	}
	assertFileHolds(t, ledgerPath, "{not json")
	assertAbsent(t, filepath.Join(stateDir, "outbox"))
	assertNoLeftovers(t, stateDir)
}

func TestABatchThatCannotBeMovedAsideFailsPendingAndStaysPut(t *testing.T) {
	stateDir := dirForFileAtPathMax(t, filepath.Join("outbox", "00000001.json"))
	s, err := usagestate.Open(stateDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	batch := filepath.Join(stateDir, "outbox", "00000001.json")
	writeFile(t, batch, "garbage")

	if bs, err := s.Pending(); err == nil {
		t.Errorf("Pending = %+v, want an error", bs)
	}
	assertFileHolds(t, batch, "garbage")
	assertNoLeftovers(t, filepath.Join(stateDir, "outbox"))
}
