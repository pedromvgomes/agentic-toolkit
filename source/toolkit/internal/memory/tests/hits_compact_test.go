package tests

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

func shardPath(s *memory.Store, name string) string {
	return filepath.Join(s.HitShardsPath(), name)
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return true
	case errors.Is(err, fs.ErrNotExist):
		return false
	}
	t.Fatalf("stat %s: %v", path, err)
	return false
}

func readRecord(t *testing.T, path string) memory.HitCounts {
	t.Helper()
	c, err := memory.ReadHitRecord(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return c
}

// TestCompactHitsMergesTheShardsIntoTheCompactedFile: the compacted file
// afterwards is its old reads plus every shard's — counts summed, the
// earliest first and the latest last — and the shards it folded are gone.
func TestCompactHitsMergesTheShardsIntoTheCompactedFile(t *testing.T) {
	s := stampedStore(t)
	writeRecord(t, s.CompactedHitsPath(), memory.HitCounts{
		"a":    {Count: 4, First: day(3, 0), Last: day(5, 0)},
		"kept": {Count: 7, First: day(1, 0), Last: day(9, 0)},
	})
	writeRecord(t, shardPath(s, "1.json"), memory.HitCounts{
		"a": {Count: 2, First: day(2, 0), Last: day(4, 0)},
	})
	writeRecord(t, shardPath(s, "2.json"), memory.HitCounts{
		"a":   {Count: 1, First: day(4, 0), Last: day(8, 0)},
		"new": {Count: 3, First: day(6, 0), Last: day(7, 0)},
	})

	c, err := s.CompactHits()
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	want := []string{shardPath(s, "1.json"), shardPath(s, "2.json")}
	if !slices.Equal(c.Folded, want) {
		t.Errorf("folded = %v, want %v", c.Folded, want)
	}
	if len(c.Skipped) != 0 {
		t.Errorf("skipped = %v, want none", c.Skipped)
	}

	got := readRecord(t, s.CompactedHitsPath())
	wantCounts := memory.HitCounts{
		"a":    {Count: 7, First: day(2, 0), Last: day(8, 0)},
		"kept": {Count: 7, First: day(1, 0), Last: day(9, 0)},
		"new":  {Count: 3, First: day(6, 0), Last: day(7, 0)},
	}
	if !reflect.DeepEqual(got, wantCounts) {
		t.Errorf("compacted = %+v, want %+v", got, wantCounts)
	}
	for _, p := range want {
		if exists(t, p) {
			t.Errorf("folded shard %s is still in %s", p, memory.HitsDir)
		}
	}
}

// TestCompactHitsCreatesTheCompactedFileFromOneShard: with no compacted file
// yet, one shard becomes it.
func TestCompactHitsCreatesTheCompactedFileFromOneShard(t *testing.T) {
	s := stampedStore(t)
	only := memory.HitCounts{"a": {Count: 2, First: day(1, 0), Last: day(2, 0)}}
	writeRecord(t, shardPath(s, "only.json"), only)

	c, err := s.CompactHits()
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if len(c.Folded) != 1 {
		t.Errorf("folded = %v, want the one shard", c.Folded)
	}
	if got := readRecord(t, s.CompactedHitsPath()); !reflect.DeepEqual(got, only) {
		t.Errorf("compacted = %+v, want the shard's reads %+v", got, only)
	}
	if exists(t, shardPath(s, "only.json")) {
		t.Error("the folded shard is still in place")
	}
	info, err := os.Stat(s.CompactedHitsPath())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("compacted file mode = %v, want 0644", info.Mode().Perm())
	}
}

// TestCompactHitsWithNoShardsWritesNothing: nothing to compact never creates
// the compacted file, and never rewrites one already there.
func TestCompactHitsWithNoShardsWritesNothing(t *testing.T) {
	t.Run("no compacted file", func(t *testing.T) {
		s := stampedStore(t)
		write(t, filepath.Join(s.HitShardsPath(), memory.GitkeepFile), "")
		c, err := s.CompactHits()
		if err != nil {
			t.Fatalf("compact: %v", err)
		}
		if len(c.Folded) != 0 || len(c.Skipped) != 0 {
			t.Errorf("compaction = %+v, want nothing done", c)
		}
		if exists(t, s.CompactedHitsPath()) {
			t.Error("compacting no shards created the compacted file")
		}
	})
	t.Run("a compacted file", func(t *testing.T) {
		s := stampedStore(t)
		body := `{"version": 1, "notes": {"a": {"count": 1}}}`
		write(t, s.CompactedHitsPath(), body)
		if _, err := s.CompactHits(); err != nil {
			t.Fatalf("compact: %v", err)
		}
		if got := read(t, s.CompactedHitsPath()); got != body {
			t.Errorf("compacted file rewritten with no shards: %q, want %q", got, body)
		}
	})
}

// TestCompactHitsLeavesAnUnreadableShardInPlace: a shard that cannot be read
// is reported and kept, and does not stop the readable ones being folded.
func TestCompactHitsLeavesAnUnreadableShardInPlace(t *testing.T) {
	s := stampedStore(t)
	bad := shardPath(s, "bad.json")
	write(t, bad, "{")
	good := memory.HitCounts{"a": {Count: 1, First: day(1, 0), Last: day(1, 0)}}
	writeRecord(t, shardPath(s, "good.json"), good)

	c, err := s.CompactHits()
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if len(c.Skipped) != 1 || !strings.Contains(c.Skipped[0].Error(), bad) {
		t.Errorf("skipped = %v, want the unreadable shard named", c.Skipped)
	}
	if !slices.Equal(c.Folded, []string{shardPath(s, "good.json")}) {
		t.Errorf("folded = %v, want the readable shard", c.Folded)
	}
	if got := read(t, bad); got != "{" {
		t.Errorf("unreadable shard = %q, want it untouched", got)
	}
	if exists(t, shardPath(s, "good.json")) {
		t.Error("the readable shard was not removed")
	}
	if got := readRecord(t, s.CompactedHitsPath()); !reflect.DeepEqual(got, good) {
		t.Errorf("compacted = %+v, want the readable shard's reads", got)
	}
}

// TestCompactHitsWithOnlyUnreadableShardsWritesNothing: shards none of which
// can be read leave nothing to fold, so the compacted file is not created.
func TestCompactHitsWithOnlyUnreadableShardsWritesNothing(t *testing.T) {
	s := stampedStore(t)
	write(t, shardPath(s, "bad.json"), "{")

	c, err := s.CompactHits()
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if len(c.Folded) != 0 || len(c.Skipped) != 1 {
		t.Errorf("compaction = %+v, want the one shard skipped and none folded", c)
	}
	if exists(t, s.CompactedHitsPath()) {
		t.Error("compacting only unreadable shards created the compacted file")
	}
	if !exists(t, shardPath(s, "bad.json")) {
		t.Error("the unreadable shard was removed")
	}
}

// TestCompactHitsRefusesAnUnreadableCompactedFile: replacing a compacted file
// nobody could read would discard its reads, so compaction stops before
// writing and every shard stays.
func TestCompactHitsRefusesAnUnreadableCompactedFile(t *testing.T) {
	s := stampedStore(t)
	conflicted := "<<<<<<< HEAD\n{}\n=======\n{}\n>>>>>>> other\n"
	write(t, s.CompactedHitsPath(), conflicted)
	writeRecord(t, shardPath(s, "1.json"), memory.HitCounts{"a": {Count: 1}})

	c, err := s.CompactHits()
	if err == nil {
		t.Fatal("compaction over an unreadable compacted file succeeded")
	}
	if !strings.Contains(err.Error(), s.CompactedHitsPath()) {
		t.Errorf("error %q does not name the compacted file", err)
	}
	if len(c.Folded) != 0 {
		t.Errorf("folded = %v, want nothing", c.Folded)
	}
	if got := read(t, s.CompactedHitsPath()); got != conflicted {
		t.Errorf("compacted file = %q, want it untouched", got)
	}
	if !exists(t, shardPath(s, "1.json")) {
		t.Error("a shard was removed when nothing was compacted")
	}
}

// TestCompactHitsKeepsTheSharedCounts: `stats` reports the same union of the
// compacted file, the shards and the local log before and after compaction.
func TestCompactHitsKeepsTheSharedCounts(t *testing.T) {
	s := stampedStore(t)
	writeRecord(t, s.CompactedHitsPath(), memory.HitCounts{"pins-shas": {Count: 2, First: day(2, 0), Last: day(3, 0)}})
	writeRecord(t, shardPath(s, "1.json"), memory.HitCounts{"pins-shas": {Count: 1, First: day(1, 0), Last: day(1, 0)}})
	writeRecord(t, shardPath(s, "2.json"), memory.HitCounts{"gone": {Count: 5, First: day(4, 0), Last: day(6, 0)}})
	recordHits(t, s, memory.Hit{Note: "pins-shas", At: day(7, 0)})

	notes, errs := s.LoadNotes()
	if len(errs) > 0 {
		t.Fatalf("load notes: %v", errs)
	}
	before, err := s.Stats(notes)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if _, err := s.CompactHits(); err != nil {
		t.Fatalf("compact: %v", err)
	}
	after, err := s.Stats(notes)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if before.Hits != 9 || after.Hits != before.Hits {
		t.Errorf("hits = %d before, %d after; want 9 both times", before.Hits, after.Hits)
	}
	if after.NotesHit != before.NotesHit || after.LocalHits != before.LocalHits {
		t.Errorf("after = %d notes hit, %d local; before = %d, %d", after.NotesHit, after.LocalHits, before.NotesHit, before.LocalHits)
	}
	if !after.FirstHit.Equal(day(1, 0)) || !after.LastHit.Equal(day(7, 0)) {
		t.Errorf("window after = %v..%v, want %v..%v", after.FirstHit, after.LastHit, day(1, 0), day(7, 0))
	}
	if before.Shards != 2 || after.Shards != 0 || !after.Compacted {
		t.Errorf("shards %d -> %d, compacted %v; want 2 -> 0 and the compacted file read", before.Shards, after.Shards, after.Compacted)
	}
}

// TestReplaceHitRecordReplacesInPlace: an existing record is replaced whole,
// and no temporary file is left beside it.
func TestReplaceHitRecordReplacesInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, memory.CompactedHitsFile)
	writeRecord(t, path, memory.HitCounts{"old": {Count: 1}})

	next := memory.HitCounts{"new": {Count: 2, First: day(1, 0), Last: day(2, 0)}}
	if err := memory.ReplaceHitRecord(path, next); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if got := readRecord(t, path); !reflect.DeepEqual(got, next) {
		t.Errorf("record = %+v, want %+v", got, next)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Errorf("directory holds %v (%v), want the record alone", entries, err)
	}
}

// TestReplaceHitRecordCleansUpAFailedWrite: a rename that cannot land leaves
// the target as it was and no temporary file behind.
func TestReplaceHitRecordCleansUpAFailedWrite(t *testing.T) {
	dir := t.TempDir()
	// A non-empty directory cannot be renamed over, however privileged the
	// test runs.
	path := filepath.Join(dir, memory.CompactedHitsFile)
	write(t, filepath.Join(path, "inside"), "x")

	if err := memory.ReplaceHitRecord(path, memory.HitCounts{"a": {Count: 1}}); err == nil {
		t.Fatal("replacing a directory succeeded")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != memory.CompactedHitsFile {
		t.Errorf("directory holds %v (%v), want the target alone", entries, err)
	}
	if got := read(t, filepath.Join(path, "inside")); got != "x" {
		t.Errorf("target contents = %q, want them untouched", got)
	}
}
