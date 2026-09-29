package tests

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

// TestStatsShape counts what a store holds, including the files a glob
// anchor currently covers.
func TestStatsShape(t *testing.T) {
	s := stampedStore(t)
	write(t, filepath.Join(s.CandidatesPath(), "unpromoted.md"), "---\nname: unpromoted\n---\n\nbody\n")
	notes, _ := s.LoadNotes()

	st, err := s.Stats(notes)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.Notes != 1 || st.ByKind[memory.KindInvariant] != 1 || st.ByConfidence[memory.ConfidenceVerified] != 1 {
		t.Errorf("counts wrong: %+v", st)
	}
	if st.Anchors != 2 || st.AnchoredFile != 3 {
		t.Errorf("anchors = %d over %d files, want 2 over 3", st.Anchors, st.AnchoredFile)
	}
	if st.Stale != 0 {
		t.Errorf("stale = %d, want 0", st.Stale)
	}
	if st.Candidates != 1 {
		t.Errorf("candidates = %d, want 1", st.Candidates)
	}
}

// TestStatsHitRate: the rate counts notes read at least once, over notes
// currently in the store.
func TestStatsHitRate(t *testing.T) {
	s := stampedStore(t)
	writeNote(t, s, "never-read", note("never-read", "  - path: internal/resolver/graph.go\n    blob: 0123456789ab\n"))
	now := time.Now()
	for i := 0; i < 3; i++ {
		if err := s.RecordHit("pins-shas", now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("record hit: %v", err)
		}
	}

	notes, _ := s.LoadNotes()
	st, err := s.Stats(notes)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.Hits != 3 || st.NotesHit != 1 || st.HitRate != 0.5 {
		t.Errorf("hits=%d notesHit=%d rate=%v, want 3 / 1 / 0.5", st.Hits, st.NotesHit, st.HitRate)
	}
	if !st.FirstHit.Before(st.LastHit) {
		t.Errorf("hit window not ordered: %s .. %s", st.FirstHit, st.LastHit)
	}
}

// TestStatsIgnoresHitsOnPrunedNotes: a hit on a note that no longer exists
// says nothing about whether today's index is repaid.
func TestStatsIgnoresHitsOnPrunedNotes(t *testing.T) {
	s := stampedStore(t)
	if err := s.RecordHit("deleted-long-ago", time.Now()); err != nil {
		t.Fatalf("record hit: %v", err)
	}

	notes, _ := s.LoadNotes()
	st, err := s.Stats(notes)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.NotesHit != 0 || st.HitRate != 0 {
		t.Errorf("pruned-note hit counted: notesHit=%d rate=%v", st.NotesHit, st.HitRate)
	}
	if st.Hits != 1 {
		t.Errorf("raw hit count = %d, want 1", st.Hits)
	}
}

// TestHitsSurviveTornLine: the log is append-only local telemetry, so a
// half-written line must not take out `stats`.
func TestHitsSurviveTornLine(t *testing.T) {
	s := stampedStore(t)
	if err := s.RecordHit("pins-shas", time.Now()); err != nil {
		t.Fatalf("record hit: %v", err)
	}
	write(t, s.HitsPath(), read(t, s.HitsPath())+"{\"note\":\"tor\n")

	hits, err := s.Hits()
	if err != nil {
		t.Fatalf("hits: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("hits = %+v, want the one intact line", hits)
	}
}

// TestStatsCountsDistinctAnchoredFiles: the label says files, so a file
// anchored twice must count once.
func TestStatsCountsDistinctAnchoredFiles(t *testing.T) {
	s := project(t, map[string]string{
		"internal/lockfile/types.go":  "package lockfile\n",
		"internal/lockfile/parser.go": "package lockfile\n",
	})
	writeNote(t, s, "overlap", note("overlap",
		"  - path: internal/lockfile/types.go\n  - path: internal/lockfile/*.go\n"))
	if _, err := s.Stamp(loadOne(t, s, "overlap")); err != nil {
		t.Fatalf("stamp: %v", err)
	}

	notes, _ := s.LoadNotes()
	st, err := s.Stats(notes)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.Anchors != 2 {
		t.Errorf("anchors = %d, want 2", st.Anchors)
	}
	if st.AnchoredFile != 2 {
		t.Errorf("anchored files = %d, want 2 distinct (types.go counted once)", st.AnchoredFile)
	}
}

// TestRecordHitWritesGitignore: `show` can be the first command run against
// a hand-created store, and the hits log must never appear without it.
func TestRecordHitWritesGitignore(t *testing.T) {
	root := t.TempDir()
	s := memory.New(root, "")
	if err := s.RecordHit("anything", time.Now()); err != nil {
		t.Fatalf("record hit: %v", err)
	}

	ignore := read(t, filepath.Join(s.Root, memory.GitignoreFile))
	if !strings.Contains(ignore, memory.HitsFile) {
		t.Errorf(".gitignore = %q, want it to cover %s", ignore, memory.HitsFile)
	}
}

// TestStatsNamesColdNotes: a low hit rate says the store is not repaying its
// cost; the cold list is what says which notes to drop, so it must name the
// unread ones and only those.
func TestStatsNamesColdNotes(t *testing.T) {
	s := stampedStore(t)
	writeNote(t, s, "never-read", note("never-read", "  - path: internal/resolver/graph.go\n    blob: 0123456789ab\n"))
	writeNote(t, s, "also-never-read", note("also-never-read", "  - path: internal/resolver/graph.go\n    blob: 0123456789ab\n"))
	if err := s.RecordHit("pins-shas", time.Now()); err != nil {
		t.Fatalf("record hit: %v", err)
	}

	notes, _ := s.LoadNotes()
	st, err := s.Stats(notes)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if got := strings.Join(st.Cold, ","); got != "also-never-read,never-read" {
		t.Errorf("cold = %q, want the two unread notes in sorted order", got)
	}
}

// TestStatsColdIgnoresHitsOnPrunedNotes: hits are matched by name, and a hit
// on a note that is gone must not warm a live note that happens to be the
// only one left.
func TestStatsColdIgnoresHitsOnPrunedNotes(t *testing.T) {
	s := stampedStore(t)
	if err := s.RecordHit("deleted-long-ago", time.Now()); err != nil {
		t.Fatalf("record hit: %v", err)
	}

	notes, _ := s.LoadNotes()
	st, err := s.Stats(notes)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if got := strings.Join(st.Cold, ","); got != "pins-shas" {
		t.Errorf("cold = %q, want the one live unread note", got)
	}
}

// TestStatsReportsIndexSizeAsTheTax: the index is what an explorer loads
// before it has decided any note is relevant, so its size is the cost the hit
// rate is judged against. Reported from the generated file, not estimated
// from the notes.
func TestStatsReportsIndexSizeAsTheTax(t *testing.T) {
	s := stampedStore(t)
	notes, _ := s.LoadNotes()

	st, err := s.Stats(notes)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.IndexBytes != 0 {
		t.Fatalf("IndexBytes = %d before any index is generated, want 0", st.IndexBytes)
	}

	if _, err := s.WriteIndex(notes); err != nil {
		t.Fatalf("write index: %v", err)
	}
	st, err = s.Stats(notes)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	want := int64(len(read(t, s.IndexPath())))
	if st.IndexBytes != want {
		t.Errorf("IndexBytes = %d, want %d (the generated index's size)", st.IndexBytes, want)
	}
}

// TestStatsUnionsCompactedShardsAndLog: the hit record is the compacted file,
// every shard and the local log together — counts summed, the window the
// widest across all three, and the rate and cold list computed over the union.
func TestStatsUnionsCompactedShardsAndLog(t *testing.T) {
	s := stampedStore(t)
	writeNote(t, s, "never-read", note("never-read", "  - path: internal/resolver/graph.go\n    blob: 0123456789ab\n"))
	writeNote(t, s, "shard-only", note("shard-only", "  - path: internal/resolver/graph.go\n    blob: 0123456789ab\n"))
	writeRecord(t, s.CompactedHitsPath(), memory.HitCounts{
		"pins-shas": {Count: 2, First: day(2, 0), Last: day(5, 0)},
	})
	writeRecord(t, filepath.Join(s.HitShardsPath(), "2026-01-10-main-aaaa.json"), memory.HitCounts{
		"pins-shas":  {Count: 1, First: day(3, 0), Last: day(10, 0)},
		"shard-only": {Count: 3, First: day(1, 0), Last: day(4, 0)},
	})
	writeRecord(t, filepath.Join(s.HitShardsPath(), "2026-01-06-other-bbbb.json"), memory.HitCounts{
		"shard-only": {Count: 1, First: day(6, 0), Last: day(6, 0)},
	})
	recordHits(t, s, memory.Hit{Note: "pins-shas", At: day(7, 0)})

	notes, _ := s.LoadNotes()
	st, err := s.Stats(notes)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.Hits != 8 {
		t.Errorf("hits = %d, want 8 (2 compacted + 4 + 1 in shards + 1 local)", st.Hits)
	}
	if st.NotesHit != 2 || st.HitRate != 2.0/3.0 {
		t.Errorf("notesHit=%d rate=%v, want 2 / 2/3", st.NotesHit, st.HitRate)
	}
	if !st.FirstHit.Equal(day(1, 0)) || !st.LastHit.Equal(day(10, 0)) {
		t.Errorf("window = %s .. %s, want day 1 .. day 10", st.FirstHit, st.LastHit)
	}
	if got := strings.Join(st.Cold, ","); got != "never-read" {
		t.Errorf("cold = %q, want the one note no record reads", got)
	}
	if st.LocalHits != 1 || st.Shards != 2 || !st.Compacted {
		t.Errorf("local=%d shards=%d compacted=%v, want 1 / 2 / true", st.LocalHits, st.Shards, st.Compacted)
	}
	if len(st.UnreadableHits) != 0 {
		t.Errorf("unreadable = %v, want none", st.UnreadableHits)
	}
}

// TestStatsCountsAFoldedLogOnce: folding moves reads from the log into a
// shard, so the totals before and after a fold are the same.
func TestStatsCountsAFoldedLogOnce(t *testing.T) {
	s := stampedStore(t)
	recordHits(t, s, memory.Hit{Note: "pins-shas", At: day(1, 0)}, memory.Hit{Note: "pins-shas", At: day(2, 0)})
	notes, _ := s.LoadNotes()
	before, err := s.Stats(notes)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if _, err := s.FoldHits(memory.FoldOptions{Now: day(3, 0), Branch: "main"}); err != nil {
		t.Fatalf("fold: %v", err)
	}
	after, err := s.Stats(notes)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if before.Hits != 2 || after.Hits != 2 || after.NotesHit != 1 {
		t.Errorf("hits before=%d after=%d notesHit=%d, want 2 / 2 / 1", before.Hits, after.Hits, after.NotesHit)
	}
	if before.LocalHits != 2 || after.LocalHits != 0 || before.Shards != 0 || after.Shards != 1 {
		t.Errorf("local %d -> %d, shards %d -> %d, want 2 -> 0 and 0 -> 1",
			before.LocalHits, after.LocalHits, before.Shards, after.Shards)
	}
	if !after.FirstHit.Equal(day(1, 0)) || !after.LastHit.Equal(day(2, 0)) {
		t.Errorf("window after fold = %s .. %s, want the reads' own window", after.FirstHit, after.LastHit)
	}
}

// TestStatsIgnoresShardsForDeletedNotes: a committed record outlives the
// notes it names, and a pruned note in it must neither warm a live note nor
// raise the rate.
func TestStatsIgnoresShardsForDeletedNotes(t *testing.T) {
	s := stampedStore(t)
	writeRecord(t, filepath.Join(s.HitShardsPath(), "2026-01-01-main-aaaa.json"), memory.HitCounts{
		"deleted-long-ago": {Count: 4, First: day(1, 0), Last: day(1, 0)},
	})

	notes, _ := s.LoadNotes()
	st, err := s.Stats(notes)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.NotesHit != 0 || st.HitRate != 0 {
		t.Errorf("deleted-note shard counted: notesHit=%d rate=%v", st.NotesHit, st.HitRate)
	}
	if got := strings.Join(st.Cold, ","); got != "pins-shas" {
		t.Errorf("cold = %q, want the one live note", got)
	}
	if st.Hits != 4 {
		t.Errorf("raw hit count = %d, want 4", st.Hits)
	}
}

// TestStatsSkipsAnUnreadableHitRecord: one malformed shard or compacted file
// is reported and left out, and the rest of the record still counts.
func TestStatsSkipsAnUnreadableHitRecord(t *testing.T) {
	s := stampedStore(t)
	bad := filepath.Join(s.HitShardsPath(), "2026-01-01-main-bad.json")
	write(t, bad, "{not json")
	write(t, s.CompactedHitsPath(), `{"version": 99, "notes": {}}`)
	writeRecord(t, filepath.Join(s.HitShardsPath(), "2026-01-02-main-good.json"), memory.HitCounts{
		"pins-shas": {Count: 2, First: day(2, 0), Last: day(2, 0)},
	})

	notes, _ := s.LoadNotes()
	st, err := s.Stats(notes)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.Hits != 2 || st.NotesHit != 1 || st.Shards != 1 || st.Compacted {
		t.Errorf("hits=%d notesHit=%d shards=%d compacted=%v, want 2 / 1 / 1 / false",
			st.Hits, st.NotesHit, st.Shards, st.Compacted)
	}
	if len(st.UnreadableHits) != 2 {
		t.Fatalf("unreadable = %v, want the bad shard and the compacted file", st.UnreadableHits)
	}
	joined := st.UnreadableHits[0].Error() + "\n" + st.UnreadableHits[1].Error()
	if !strings.Contains(joined, bad) || !strings.Contains(joined, s.CompactedHitsPath()) {
		t.Errorf("unreadable records not named: %s", joined)
	}
}

// TestStatsWithOnlyTheLocalLog: with no committed record, every hit is this
// checkout's, and Shards and Compacted say so.
func TestStatsWithOnlyTheLocalLog(t *testing.T) {
	s := stampedStore(t)
	recordHits(t, s, memory.Hit{Note: "pins-shas", At: day(1, 0)})

	notes, _ := s.LoadNotes()
	st, err := s.Stats(notes)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.Hits != 1 || st.LocalHits != 1 || st.Shards != 0 || st.Compacted {
		t.Errorf("hits=%d local=%d shards=%d compacted=%v, want 1 / 1 / 0 / false",
			st.Hits, st.LocalHits, st.Shards, st.Compacted)
	}
}

// TestStatsWindowSkipsAnUnknownFirstRead: a record that does not say when a
// note was first read leaves the window's start to the records that do.
func TestStatsWindowSkipsAnUnknownFirstRead(t *testing.T) {
	s := stampedStore(t)
	writeRecord(t, filepath.Join(s.HitShardsPath(), "2026-01-05-main-aaaa.json"), memory.HitCounts{
		"pins-shas": {Count: 1, Last: day(5, 0)},
	})
	recordHits(t, s, memory.Hit{Note: "pruned", At: day(3, 0)})

	notes, _ := s.LoadNotes()
	st, err := s.Stats(notes)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if !st.FirstHit.Equal(day(3, 0)) || !st.LastHit.Equal(day(5, 0)) {
		t.Errorf("window = %s .. %s, want day 3 .. day 5", st.FirstHit, st.LastHit)
	}
}
