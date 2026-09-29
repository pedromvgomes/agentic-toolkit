package memory

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func compactFixture(t *testing.T) (*Store, []string, HitCounts) {
	t.Helper()
	s := &Store{Root: t.TempDir()}
	if err := os.MkdirAll(s.HitShardsPath(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	at := func(d int) time.Time { return time.Date(2026, time.January, d, 0, 0, 0, 0, time.UTC) }
	if err := WriteHitRecord(s.CompactedHitsPath(), HitCounts{"a": {Count: 4, First: at(3), Last: at(5)}}); err != nil {
		t.Fatalf("write compacted: %v", err)
	}
	shards := []string{filepath.Join(s.HitShardsPath(), "1.json"), filepath.Join(s.HitShardsPath(), "2.json")}
	if err := WriteHitRecord(shards[0], HitCounts{"a": {Count: 2, First: at(1), Last: at(4)}}); err != nil {
		t.Fatalf("write shard: %v", err)
	}
	if err := WriteHitRecord(shards[1], HitCounts{"b": {Count: 1, First: at(6), Last: at(6)}}); err != nil {
		t.Fatalf("write shard: %v", err)
	}
	merged := HitCounts{
		"a": {Count: 6, First: at(1), Last: at(5)},
		"b": {Count: 1, First: at(6), Last: at(6)},
	}
	return s, shards, merged
}

func replaceRemoveShard(t *testing.T, fn func(string) error) {
	t.Helper()
	orig := removeShard
	removeShard = fn
	t.Cleanup(func() { removeShard = orig })
}

// TestCompactHitsRemovesNoShardBeforeTheCompactedFileHoldsItsReads: every
// removal happens once the compacted file already holds the merged reads, so
// a compaction stopped at any point loses none of them.
func TestCompactHitsRemovesNoShardBeforeTheCompactedFileHoldsItsReads(t *testing.T) {
	s, shards, merged := compactFixture(t)
	var removed []string
	replaceRemoveShard(t, func(p string) error {
		got, err := ReadHitRecord(s.CompactedHitsPath())
		if err != nil || !reflect.DeepEqual(got, merged) {
			t.Errorf("removing %s while the compacted file holds %+v (%v), want %+v", p, got, err, merged)
		}
		removed = append(removed, p)
		return os.Remove(p)
	})

	if _, err := s.CompactHits(); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !reflect.DeepEqual(removed, shards) {
		t.Errorf("removed %v, want every folded shard %v", removed, shards)
	}
}

// TestCompactHitsNamesAShardItCouldNotRemove: a shard whose removal fails is
// named in the error, with the cause, and left in place, while the compacted
// file keeps the merged reads and the other shards are removed. A shard
// already gone is removed as far as compaction is concerned.
func TestCompactHitsNamesAShardItCouldNotRemove(t *testing.T) {
	s, shards, merged := compactFixture(t)
	refused := errors.New("refused")
	replaceRemoveShard(t, func(p string) error {
		switch p {
		case shards[0]:
			return fmt.Errorf("remove %s: %w", p, refused)
		case shards[1]:
			if err := os.Remove(p); err != nil {
				return err
			}
			return fmt.Errorf("remove %s: %w", p, fs.ErrNotExist)
		}
		return os.Remove(p)
	})

	c, err := s.CompactHits()
	if err == nil {
		t.Fatal("compaction that could not remove a shard succeeded")
	}
	if !errors.Is(err, refused) {
		t.Errorf("error %q does not carry the removal's cause", err)
	}
	if !strings.Contains(err.Error(), shards[0]) || strings.Contains(err.Error(), shards[1]) {
		t.Errorf("error %q, want it to name %s alone", err, shards[0])
	}
	if !reflect.DeepEqual(c.Folded, shards) {
		t.Errorf("folded = %v, want %v", c.Folded, shards)
	}
	if got, err := ReadHitRecord(s.CompactedHitsPath()); err != nil || !reflect.DeepEqual(got, merged) {
		t.Errorf("compacted = %+v (%v), want %+v", got, err, merged)
	}
	if _, err := os.Stat(shards[0]); err != nil {
		t.Errorf("the shard that could not be removed is gone: %v", err)
	}
	if _, err := os.Stat(shards[1]); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the removable shard is still in place: %v", err)
	}
}
