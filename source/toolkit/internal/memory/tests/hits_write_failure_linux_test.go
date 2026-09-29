package tests

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

// underFileSizeLimit runs fn with the process's file size limit at limit
// bytes, so a write that would grow a file past it fails with EFBIG. The
// limit binds a process running as root as well, which a permission bit does
// not, and the Go runtime ignores the SIGXFSZ that comes with the failure.
// Every file the process writes is held to it while fn runs, so fn must not
// log: no test in this package is parallel, and nothing else writes.
func underFileSizeLimit(t *testing.T, limit uint64, fn func()) {
	t.Helper()
	var old syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
		t.Fatalf("read the file size limit: %v", err)
	}
	if old.Max < limit {
		t.Skipf("the hard file size limit %d is below %d", old.Max, limit)
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: limit, Max: old.Max}); err != nil {
		t.Fatalf("lower the file size limit: %v", err)
	}
	defer func() {
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
			t.Fatalf("restore the file size limit: %v", err)
		}
	}()
	fn()
}

// openDescriptors is how many file descriptors the process holds.
func openDescriptors(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("list descriptors: %v", err)
	}
	return len(entries)
}

// TestWriteHitRecordRemovesAPartialWrite: a record that cannot be written
// whole leaves no file at its path, so the name stays free and no reader
// finds a truncated record, and no descriptor stays open.
func TestWriteHitRecordRemovesAPartialWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shard.json")
	counts := memory.HitCounts{"a": {Count: 1, First: day(1, 0), Last: day(1, 0)}}
	before := openDescriptors(t)

	var err error
	underFileSizeLimit(t, 8, func() { err = memory.WriteHitRecord(path, counts) })

	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("write past the limit = %v, want an error naming %s", err, path)
	}
	if exists(t, path) {
		t.Errorf("a partial record is left at %s: %q", path, read(t, path))
	}
	if after := openDescriptors(t); after != before {
		t.Errorf("%d descriptors open after a failed write, want %d", after, before)
	}
}

// TestReplaceHitRecordKeepsTheOldRecordWhenTheWriteFails: a replacement that
// cannot be written leaves the old record whole, no temporary file beside it
// and no descriptor open.
func TestReplaceHitRecordKeepsTheOldRecordWhenTheWriteFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, memory.CompactedHitsFile)
	writeRecord(t, path, memory.HitCounts{"old": {Count: 1, First: day(1, 0), Last: day(1, 0)}})
	old := read(t, path)
	before := openDescriptors(t)

	var err error
	underFileSizeLimit(t, 8, func() {
		err = memory.ReplaceHitRecord(path, memory.HitCounts{"new": {Count: 2, First: day(2, 0), Last: day(2, 0)}})
	})

	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("replace past the limit = %v, want an error naming %s", err, path)
	}
	if got := read(t, path); got != old {
		t.Errorf("record = %q, want the old one %q", got, old)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Errorf("directory holds %v (%v), want the record alone", entries, err)
	}
	if after := openDescriptors(t); after != before {
		t.Errorf("%d descriptors open after a failed replace, want %d", after, before)
	}
}

// TestCompactHitsThatCannotWriteRemovesNoShard: the compacted file is written
// before any shard is removed, so a write that fails leaves every shard and
// the compacted file as they were, and still reports the unreadable shards.
func TestCompactHitsThatCannotWriteRemovesNoShard(t *testing.T) {
	s := stampedStore(t)
	writeRecord(t, s.CompactedHitsPath(), memory.HitCounts{"a": {Count: 4, First: day(3, 0), Last: day(5, 0)}})
	compacted := read(t, s.CompactedHitsPath())
	shards := []string{shardPath(s, "1.json"), shardPath(s, "2.json")}
	writeRecord(t, shards[0], memory.HitCounts{"a": {Count: 2, First: day(1, 0), Last: day(4, 0)}})
	writeRecord(t, shards[1], memory.HitCounts{"b": {Count: 1, First: day(6, 0), Last: day(6, 0)}})
	bad := shardPath(s, "bad.json")
	write(t, bad, "{")

	var (
		c   memory.Compaction
		err error
	)
	underFileSizeLimit(t, 8, func() { c, err = s.CompactHits() })

	if err == nil || !strings.Contains(err.Error(), s.CompactedHitsPath()) {
		t.Errorf("compaction past the limit = %v, want an error naming the compacted file", err)
	}
	if len(c.Folded) != 0 {
		t.Errorf("folded = %v, want nothing reported folded", c.Folded)
	}
	if len(c.Skipped) != 1 || !strings.Contains(c.Skipped[0].Error(), bad) {
		t.Errorf("skipped = %v, want the unreadable shard named", c.Skipped)
	}
	for _, p := range append(shards, bad) {
		if !exists(t, p) {
			t.Errorf("shard %s was removed by a compaction that wrote nothing", p)
		}
	}
	if got := read(t, s.CompactedHitsPath()); got != compacted {
		t.Errorf("compacted file = %q, want it untouched %q", got, compacted)
	}
	got, err := memory.ReadHitRecord(shards[0])
	if err != nil || !reflect.DeepEqual(got, memory.HitCounts{"a": {Count: 2, First: day(1, 0), Last: day(4, 0)}}) {
		t.Errorf("shard %s = %+v (%v), want its reads untouched", shards[0], got, err)
	}
}
