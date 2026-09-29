package tests

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/curator"
	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

// at is a UTC instant on 2026-02-<d>, for ordering reads without the clock.
func at(d int) time.Time { return time.Date(2026, time.February, d, 0, 0, 0, 0, time.UTC) }

// The two shards and the compacted file every compaction test starts from,
// and the record they merge into.
var (
	shardA    = memory.HitCounts{pinsNote: {Count: 2, First: at(3), Last: at(4)}}
	shardB    = memory.HitCounts{pinsNote: {Count: 1, First: at(2), Last: at(9)}, "other": {Count: 4, First: at(5), Last: at(6)}}
	compacted = memory.HitCounts{pinsNote: {Count: 5, First: at(1), Last: at(7)}, "old": {Count: 3, First: at(1), Last: at(8)}}
	merged    = memory.HitCounts{
		pinsNote: {Count: 8, First: at(1), Last: at(9)},
		"other":  {Count: 4, First: at(5), Last: at(6)},
		"old":    {Count: 3, First: at(1), Last: at(8)},
	}
)

func (p *project) shardPath(name string) string {
	return filepath.Join(p.store.HitShardsPath(), name+".json")
}

// writeShards stages shardA and shardB in the store's hits/.
func (p *project) writeShards(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(p.store.HitShardsPath(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, counts := range map[string]memory.HitCounts{"a": shardA, "b": shardB} {
		if err := memory.WriteHitRecord(p.shardPath(name), counts); err != nil {
			t.Fatalf("write shard %s: %v", name, err)
		}
	}
}

func (p *project) writeCompacted(t *testing.T, counts memory.HitCounts) {
	t.Helper()
	if err := memory.WriteHitRecord(p.store.CompactedHitsPath(), counts); err != nil {
		t.Fatalf("write the compacted file: %v", err)
	}
}

func present(t *testing.T, path string) bool {
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

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// wantUncompacted fails the test unless both shards are still in hits/ and
// the compacted file is exactly compactedBefore — "" for no file at all.
func wantUncompacted(t *testing.T, p *project, res curator.Result, compactedBefore string) {
	t.Helper()
	for _, name := range []string{"a", "b"} {
		if !present(t, p.shardPath(name)) {
			t.Errorf("shard %s was removed by a run that compacts nothing", name)
		}
	}
	switch {
	case compactedBefore == "" && present(t, p.store.CompactedHitsPath()):
		t.Error("a run that compacts nothing created the compacted file")
	case compactedBefore != "" && readFile(t, p.store.CompactedHitsPath()) != compactedBefore:
		t.Error("a run that compacts nothing rewrote the compacted file")
	}
	if len(res.Compaction.Folded) != 0 || len(res.Compaction.Skipped) != 0 {
		t.Errorf("Compaction = %+v, want nothing", res.Compaction)
	}
}

// Compaction is about hits, not candidates, so every job shape that passes
// verification folds the shards into the compacted file — merged with what it
// already held — and removes them.
func TestEveryVerifiedJobCompactsTheHitShards(t *testing.T) {
	for name, opts := range map[string]curator.Options{
		"the backlog run": {},
		"the stale job":   {Stale: true},
		"a scoped run":    {Notes: []string{pinsNote}},
		"a limited run":   {Limit: 1},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProject(t)
			p.writeShards(t)
			p.writeCompacted(t, compacted)

			res, err := p.curate(t, nil, curator.Report{}, opts)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			want := []string{p.shardPath("a"), p.shardPath("b")}
			if !reflect.DeepEqual(res.Compaction.Folded, want) {
				t.Errorf("Compaction.Folded = %v, want %v", res.Compaction.Folded, want)
			}
			for _, shard := range want {
				if present(t, shard) {
					t.Errorf("folded shard %s is still in hits/", shard)
				}
			}
			got, err := memory.ReadHitRecord(p.store.CompactedHitsPath())
			if err != nil {
				t.Fatalf("read the compacted file: %v", err)
			}
			if !reflect.DeepEqual(got, merged) {
				t.Errorf("compacted file = %+v, want %+v", got, merged)
			}
		})
	}
}

// A dry run's grant writes nothing, and agtk writes nothing on its behalf.
func TestADryRunCompactsNothing(t *testing.T) {
	p := newProject(t)
	p.writeShards(t)
	p.writeCompacted(t, compacted)
	before := readFile(t, p.store.CompactedHitsPath())

	res, err := p.curate(t, nil, curator.Report{}, curator.Options{DryRun: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantUncompacted(t, p, res, before)
}

// A run the store contradicts leaves the store exactly as the run left it,
// hits/ included.
func TestARunThatFailsVerificationCompactsNothing(t *testing.T) {
	p := newProject(t)
	p.writeShards(t)

	res, err := p.curate(t, nil, curator.Report{CandidatesResolved: []string{"20260101-never-staged"}}, curator.Options{})
	wantRefused(t, err, "was never staged")
	wantUncompacted(t, p, res, "")
}

// With no shard to fold there is nothing to write: the compacted file is
// neither created nor rewritten.
func TestARunOverNoShardsLeavesTheCompactedFileAlone(t *testing.T) {
	t.Run("no compacted file", func(t *testing.T) {
		p := newProject(t)
		res, err := p.curate(t, nil, curator.Report{}, curator.Options{})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if present(t, p.store.CompactedHitsPath()) {
			t.Error("a run over no shards created the compacted file")
		}
		if len(res.Compaction.Folded) != 0 {
			t.Errorf("Compaction.Folded = %v, want none", res.Compaction.Folded)
		}
	})
	t.Run("a compacted file", func(t *testing.T) {
		p := newProject(t)
		p.writeCompacted(t, compacted)
		before := readFile(t, p.store.CompactedHitsPath())
		if _, err := p.curate(t, nil, curator.Report{}, curator.Options{}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if readFile(t, p.store.CompactedHitsPath()) != before {
			t.Error("a run over no shards rewrote the compacted file")
		}
	})
}

// A shard that cannot be read is data nobody has counted, so the run leaves it
// in place and reports it, and still folds the shards it can read.
func TestAnUnreadableShardIsLeftInPlaceAndReported(t *testing.T) {
	p := newProject(t)
	p.writeShards(t)
	bad := p.shardPath("broken")
	writeFile(t, bad, "{")

	res, err := p.curate(t, nil, curator.Report{}, curator.Options{})
	if err != nil {
		t.Fatalf("an unreadable shard failed the run: %v", err)
	}
	if len(res.Compaction.Skipped) != 1 || !strings.Contains(res.Compaction.Skipped[0].Error(), bad) {
		t.Errorf("Compaction.Skipped = %v, want the unreadable shard named", res.Compaction.Skipped)
	}
	if readFile(t, bad) != "{" {
		t.Error("the unreadable shard was not left as it was")
	}
	if len(res.Compaction.Folded) != 2 || present(t, p.shardPath("a")) || present(t, p.shardPath("b")) {
		t.Errorf("Compaction.Folded = %v, want both readable shards folded and removed", res.Compaction.Folded)
	}
}

// A compacted file that cannot be read fails the run after verification, and
// the shards stay: replacing the file would discard the reads it holds.
func TestAnUnreadableCompactedFileFailsTheRunAndKeepsTheShards(t *testing.T) {
	p := newProject(t)
	p.writeShards(t)
	writeFile(t, p.store.CompactedHitsPath(), "{")

	res, err := p.curate(t, nil, curator.Report{}, curator.Options{})
	if err == nil {
		t.Fatal("a run over an unreadable compacted file succeeded")
	}
	if !strings.Contains(err.Error(), p.store.CompactedHitsPath()) {
		t.Errorf("error does not name the compacted file: %v", err)
	}
	if res.Text == "" {
		t.Error("the curator's own account was dropped from a run failed for its hit record")
	}
	wantUncompacted(t, p, res, "{")
}

// `stats` reads the compacted file and the shards as one union, so moving
// reads from one to the other changes none of its hit numbers.
func TestStatsReportsTheSameHitsAfterTheRunCompacts(t *testing.T) {
	p := newProject(t)
	p.writeNote(t, pinsNote, true)
	p.reindex(t)
	p.writeShards(t)
	p.writeCompacted(t, compacted)

	stats := func() memory.Stats {
		t.Helper()
		notes, errs := p.store.LoadNotes()
		if len(errs) > 0 {
			t.Fatalf("load notes: %v", errs)
		}
		st, err := p.store.Stats(notes)
		if err != nil {
			t.Fatalf("stats: %v", err)
		}
		return st
	}
	before := stats()
	if _, err := p.curate(t, nil, curator.Report{}, curator.Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	after := stats()

	if before.Hits != merged.Total() || after.Hits != before.Hits {
		t.Errorf("hits = %d before, %d after; want %d both times", before.Hits, after.Hits, merged.Total())
	}
	if after.NotesHit != before.NotesHit || after.NotesHit != 1 {
		t.Errorf("notes hit = %d before, %d after; want 1 both times", before.NotesHit, after.NotesHit)
	}
	if !after.FirstHit.Equal(before.FirstHit) || !after.LastHit.Equal(before.LastHit) {
		t.Errorf("window %v..%v after, want %v..%v", after.FirstHit, after.LastHit, before.FirstHit, before.LastHit)
	}
	if before.Shards != 2 || after.Shards != 0 || !after.Compacted {
		t.Errorf("shards %d -> %d, compacted %v; want 2 -> 0 with the compacted file read", before.Shards, after.Shards, after.Compacted)
	}
}
