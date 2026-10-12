//go:build unix

package usagestate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	lockFileName = "usage.lock"

	// lockWait bounds how long acquire retries before giving up. A Stop hook
	// must not hang behind another flush.
	lockWait = 2 * time.Second

	// lockPoll is the pause between non-blocking attempts.
	lockPoll = 20 * time.Millisecond
)

// ErrBusy is returned when another process holds the lock for longer than the
// bounded wait. Test for it with errors.Is.
var ErrBusy = errors.New("usagestate: another process holds the usage lock")

// lock is a held flock on the usage lock file. The kernel releases it when the
// process dies, so a crash never leaves a stale lock behind.
type lock struct {
	f *os.File
}

// acquire takes the exclusive lock in dir, creating dir when absent. It retries
// a non-blocking flock until lockWait has passed, then returns ErrBusy.
func acquire(dir string) (*lock, error) {
	if err := ensureDir(dir); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, lockFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, fileMode) // #nosec G304 -- agtk's own lock file in its state directory
	if err != nil {
		return nil, fmt.Errorf("usagestate: open %s: %w", path, err)
	}
	deadline := time.Now().Add(lockWait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) // #nosec G115 -- a file descriptor fits an int
		if err == nil {
			return &lock{f: f}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()
			return nil, fmt.Errorf("usagestate: lock %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, ErrBusy
		}
		time.Sleep(lockPoll)
	}
}

// release drops the lock. Closing the descriptor releases the flock.
func (l *lock) release() error {
	return l.f.Close()
}
