//go:build unix

package tests

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/usagestate"
)

// stageChunked stages ids[from:to] in overlapping windows of five, so
// concurrent callers race over the same ids again and again.
func stageChunked(s *usagestate.Store, fileKey string, ids []string) error {
	for i := 0; i < len(ids); i += 3 {
		end := min(i+5, len(ids))
		if _, err := s.Stage(fileKey, rows(ids[i:end]...)); err != nil {
			return fmt.Errorf("Stage %v: %w", ids[i:end], err)
		}
	}
	return nil
}

func TestConcurrentStageFromManyGoroutinesLosesAndDuplicatesNothing(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")
	all := idRange(0, 40)

	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each worker covers an overlapping window of the ids, so every
			// id is offered by several workers and the windows together
			// cover all of them.
			from := w * 3
			to := min(from+12, len(all))
			if err := stageChunked(s, file, all[from:to]); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	assertExactlyOnce(t, "pending", pendingIDs(t, mustPending(t, s), file), all)
	assertExactlyOnce(t, "ledger", ledgerOnDisk(t, stateDir)[file], all)
	if st := mustStats(t, s); len(st.Quarantined) != 0 {
		t.Errorf("concurrent writers left corrupt files: %v", st.Quarantined)
	}
}

const (
	helperEnvDir  = "USAGESTATE_TEST_HELPER_DIR"
	helperEnvFile = "USAGESTATE_TEST_HELPER_FILE"
	helperEnvFrom = "USAGESTATE_TEST_HELPER_FROM"
	helperEnvTo   = "USAGESTATE_TEST_HELPER_TO"
)

// TestHelperProcessStagesRows is the body of a child process started by
// TestConcurrentStageFromTwoProcessesLosesAndDuplicatesNothing. Run directly it
// does nothing.
func TestHelperProcessStagesRows(t *testing.T) {
	dir := os.Getenv(helperEnvDir)
	if dir == "" {
		return
	}
	from, err := strconv.Atoi(os.Getenv(helperEnvFrom))
	if err != nil {
		t.Fatalf("from: %v", err)
	}
	to, err := strconv.Atoi(os.Getenv(helperEnvTo))
	if err != nil {
		t.Fatalf("to: %v", err)
	}
	s, err := usagestate.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := stageChunked(s, os.Getenv(helperEnvFile), idRange(from, to)); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentStageFromTwoProcessesLosesAndDuplicatesNothing(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")

	// The two ranges overlap on msg_0020..msg_0039.
	ranges := [][2]int{{0, 40}, {20, 60}}
	cmds := make([]*exec.Cmd, len(ranges))
	outs := make([]*bytes.Buffer, len(ranges))
	for i, r := range ranges {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcessStagesRows$", "-test.count=1")
		cmd.Env = append(os.Environ(),
			helperEnvDir+"="+stateDir,
			helperEnvFile+"="+file,
			helperEnvFrom+"="+strconv.Itoa(r[0]),
			helperEnvTo+"="+strconv.Itoa(r[1]),
		)
		outs[i] = &bytes.Buffer{}
		cmd.Stdout = outs[i]
		cmd.Stderr = outs[i]
		cmds[i] = cmd
	}
	for i, cmd := range cmds {
		if err := cmd.Start(); err != nil {
			t.Fatalf("start helper %d: %v", i, err)
		}
	}
	for i, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Errorf("helper %d: %v\n%s", i, err, outs[i])
		}
	}

	all := idRange(0, 60)
	assertExactlyOnce(t, "pending", pendingIDs(t, mustPending(t, s), file), all)
	assertExactlyOnce(t, "ledger", ledgerOnDisk(t, stateDir)[file], all)
	if st := mustStats(t, s); len(st.Quarantined) != 0 {
		t.Errorf("concurrent processes left corrupt files: %v", st.Quarantined)
	}
}

func TestStageReturnsErrBusyWhileAnotherHolderKeepsTheLock(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")

	// A second open file description conflicts with the Store's own, so this
	// holds the lock against it even within one process.
	f, err := os.OpenFile(filepath.Join(stateDir, "usage.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("flock: %v", err)
	}

	start := time.Now()
	res, err := s.Stage(file, rows("msg_1"))
	if !errors.Is(err, usagestate.ErrBusy) {
		t.Fatalf("Stage under a held lock: err = %v, want ErrBusy", err)
	}
	if waited := time.Since(start); waited > 10*time.Second {
		t.Errorf("Stage waited %v before giving up, want a bounded wait", waited)
	}
	if res.Staged != 0 || res.Batch != 0 {
		t.Errorf("busy Stage = %+v, want nothing staged", res)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "ledger.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("busy Stage touched the Ledger: %v", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if res := mustStage(t, s, file, rows("msg_1")); res.Staged != 1 {
		t.Errorf("Stage once the lock is free = %+v, want 1 staged", res)
	}
}

func TestWaitingForAHeldLockDoesNotSpin(t *testing.T) {
	s, stateDir, tdir := newStore(t)
	file := transcript(t, tdir, "a.jsonl")

	f, err := os.OpenFile(filepath.Join(stateDir, "usage.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("flock: %v", err)
	}

	before := cpuTime(t)
	start := time.Now()
	if _, err := s.Stage(file, rows("msg_1")); !errors.Is(err, usagestate.ErrBusy) {
		t.Fatalf("Stage under a held lock: err = %v, want ErrBusy", err)
	}
	waited := time.Since(start)
	// Retrying without a pause burns a whole CPU for the full wait.
	if used := cpuTime(t) - before; used > waited/4 {
		t.Errorf("waiting %v for the lock used %v of CPU, want it to sleep between attempts", waited, used)
	}
}

// cpuTime is the user and system CPU time this process has used.
func cpuTime(t *testing.T) time.Duration {
	t.Helper()
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatalf("getrusage: %v", err)
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
