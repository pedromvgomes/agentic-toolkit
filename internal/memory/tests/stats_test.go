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
