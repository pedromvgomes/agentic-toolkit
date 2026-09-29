package tests

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

// day builds a UTC instant on 2026-01-<d> at hh:00, so tests can order reads
// without reading the clock.
func day(d, hh int) time.Time {
	return time.Date(2026, time.January, d, hh, 0, 0, 0, time.UTC)
}

// fixedSuffixes returns a Suffix func that hands out names in order and
// counts how many it was asked for.
func fixedSuffixes(names ...string) (func() string, *int) {
	calls := 0
	return func() string {
		n := names[len(names)-1]
		if calls < len(names) {
			n = names[calls]
		}
		calls++
		return n
	}, &calls
}

func recordHits(t *testing.T, s *memory.Store, hits ...memory.Hit) {
	t.Helper()
	for _, h := range hits {
		if err := s.RecordHit(h.Note, h.At); err != nil {
			t.Fatalf("record hit: %v", err)
		}
	}
}

func writeRecord(t *testing.T, path string, counts memory.HitCounts) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := memory.WriteHitRecord(path, counts); err != nil {
		t.Fatalf("write hit record: %v", err)
	}
}

// TestFoldWritesAShardAndEmptiesTheLog: a fold moves every read in the local
// log into one shard, named from the UTC date and the branch, and leaves the
// log empty so the same reads are never counted twice.
func TestFoldWritesAShardAndEmptiesTheLog(t *testing.T) {
	s := stampedStore(t)
	recordHits(t, s,
		memory.Hit{Note: "pins-shas", At: day(3, 9)},
		memory.Hit{Note: "other", At: day(2, 8)},
		memory.Hit{Note: "pins-shas", At: day(1, 7)},
	)
	suffix, _ := fixedSuffixes("abcd1234")
	// 01:00 at UTC+3 is still the previous day in UTC.
	now := time.Date(2026, time.September, 30, 1, 0, 0, 0, time.FixedZone("east", 3*3600))

	fold, err := s.FoldHits(memory.FoldOptions{Now: now, Branch: "feature/Hits_Fold", Suffix: suffix})
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	want := filepath.Join(s.HitShardsPath(), "2026-09-29-feature-hits-fold-abcd1234.json")
	if fold.Shard != want {
		t.Errorf("shard = %s, want %s", fold.Shard, want)
	}
	if fold.Hits != 3 || fold.Notes != 2 {
		t.Errorf("folded %d reads over %d notes, want 3 over 2", fold.Hits, fold.Notes)
	}

	got, err := memory.ReadHitRecord(want)
	if err != nil {
		t.Fatalf("read shard: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("shard holds %d notes, want 2: %+v", len(got), got)
	}
	if h := got["pins-shas"]; h.Count != 2 || !h.First.Equal(day(1, 7)) || !h.Last.Equal(day(3, 9)) {
		t.Errorf("pins-shas = %+v, want 2 reads from %s to %s", h, day(1, 7), day(3, 9))
	}
	if h := got["other"]; h.Count != 1 || !h.First.Equal(day(2, 8)) || !h.Last.Equal(day(2, 8)) {
		t.Errorf("other = %+v, want 1 read at %s", h, day(2, 8))
	}
	if raw := read(t, want); !strings.Contains(raw, `"version": 1`) {
		t.Errorf("shard carries no version: %s", raw)
	}

	if raw := read(t, s.HitsPath()); raw != "" {
		t.Errorf("log after fold = %q, want it empty", raw)
	}
	hits, err := s.Hits()
	if err != nil || len(hits) != 0 {
		t.Errorf("hits after fold = %v (%v), want none", hits, err)
	}
}

// TestFoldWithNothingToFoldWritesNothing: an empty or missing log is a
// successful no-op, and must not leave an empty shard behind to be committed.
func TestFoldWithNothingToFoldWritesNothing(t *testing.T) {
	for name, setup := range map[string]func(*memory.Store){
		"missing": func(*memory.Store) {},
		"empty":   func(s *memory.Store) { write(t, s.HitsPath(), "") },
		"torn":    func(s *memory.Store) { write(t, s.HitsPath(), "{\"note\":\"tor\n") },
	} {
		t.Run(name, func(t *testing.T) {
			s := stampedStore(t)
			setup(s)
			fold, err := s.FoldHits(memory.FoldOptions{Now: day(1, 0), Branch: "main"})
			if err != nil {
				t.Fatalf("fold: %v", err)
			}
			if fold != (memory.Fold{}) {
				t.Errorf("fold = %+v, want nothing folded", fold)
			}
			if _, err := os.Stat(s.HitShardsPath()); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("fold with nothing to fold created %s (%v)", s.HitShardsPath(), err)
			}
		})
	}
}

// TestFoldLeavesTheLogWhenTheShardCannotBeWritten: the shard is written
// before the log is emptied, so a fold that fails to write loses nothing.
func TestFoldLeavesTheLogWhenTheShardCannotBeWritten(t *testing.T) {
	s := stampedStore(t)
	recordHits(t, s, memory.Hit{Note: "pins-shas", At: day(1, 0)})
	before := read(t, s.HitsPath())
	// A file where the shard directory belongs fails the write however
	// privileged the test runs.
	write(t, s.HitShardsPath(), "not a directory")

	if _, err := s.FoldHits(memory.FoldOptions{Now: day(2, 0), Branch: "main"}); err == nil {
		t.Fatal("fold succeeded with no directory to write the shard into")
	}
	if after := read(t, s.HitsPath()); after != before {
		t.Errorf("log after a failed fold = %q, want it untouched: %q", after, before)
	}
}

// TestFoldsOnTheSameDayDoNotCollide: two folds on one day and branch write
// two shards, whether the names differ by chance or a taken one is retried.
func TestFoldsOnTheSameDayDoNotCollide(t *testing.T) {
	t.Run("random suffix", func(t *testing.T) {
		s := stampedStore(t)
		opts := memory.FoldOptions{Now: day(5, 12), Branch: "main"}
		var shards []string
		for i := 0; i < 2; i++ {
			recordHits(t, s, memory.Hit{Note: "pins-shas", At: day(5, i)})
			fold, err := s.FoldHits(opts)
			if err != nil {
				t.Fatalf("fold %d: %v", i, err)
			}
			shards = append(shards, fold.Shard)
		}
		if shards[0] == shards[1] {
			t.Fatalf("two folds wrote the same shard %s", shards[0])
		}
		name := regexp.MustCompile(`^2026-01-05-main-[0-9a-f]{8}\.json$`)
		for _, sh := range shards {
			if !name.MatchString(filepath.Base(sh)) {
				t.Errorf("shard name %q does not follow <date>-<branch>-<suffix>.json", filepath.Base(sh))
			}
		}
		got, err := s.HitShards()
		if err != nil || len(got) != 2 {
			t.Errorf("shards on disk = %v (%v), want both folds", got, err)
		}
	})

	t.Run("taken name is retried", func(t *testing.T) {
		s := stampedStore(t)
		suffix, calls := fixedSuffixes("aaaa", "aaaa", "bbbb")
		opts := memory.FoldOptions{Now: day(5, 12), Branch: "main", Suffix: suffix}
		recordHits(t, s, memory.Hit{Note: "pins-shas", At: day(5, 1)})
		if _, err := s.FoldHits(opts); err != nil {
			t.Fatalf("first fold: %v", err)
		}
		recordHits(t, s, memory.Hit{Note: "pins-shas", At: day(5, 2)}, memory.Hit{Note: "pins-shas", At: day(5, 3)})
		fold, err := s.FoldHits(opts)
		if err != nil {
			t.Fatalf("second fold: %v", err)
		}
		if got := filepath.Base(fold.Shard); got != "2026-01-05-main-bbbb.json" {
			t.Errorf("second shard = %s, want the retried name", got)
		}
		if *calls != 3 {
			t.Errorf("suffix drawn %d times, want 3", *calls)
		}
		first, err := memory.ReadHitRecord(filepath.Join(s.HitShardsPath(), "2026-01-05-main-aaaa.json"))
		if err != nil || first["pins-shas"].Count != 1 {
			t.Errorf("the taken shard was altered: %+v (%v)", first, err)
		}
	})
}

// TestFoldGivesUpWhenEveryNameIsTaken: a suffix source that keeps repeating
// is bounded, and the fold that gives up leaves the log as it was.
func TestFoldGivesUpWhenEveryNameIsTaken(t *testing.T) {
	s := stampedStore(t)
	suffix, calls := fixedSuffixes("same")
	opts := memory.FoldOptions{Now: day(5, 12), Branch: "main", Suffix: suffix}
	recordHits(t, s, memory.Hit{Note: "pins-shas", At: day(5, 1)})
	if _, err := s.FoldHits(opts); err != nil {
		t.Fatalf("first fold: %v", err)
	}
	recordHits(t, s, memory.Hit{Note: "pins-shas", At: day(5, 2)})
	before := read(t, s.HitsPath())
	*calls = 0

	if _, err := s.FoldHits(opts); err == nil {
		t.Fatal("fold succeeded with every shard name taken")
	}
	if *calls != 8 {
		t.Errorf("fold tried %d names, want 8", *calls)
	}
	if after := read(t, s.HitsPath()); after != before {
		t.Errorf("log after a failed fold = %q, want %q", after, before)
	}
}

// TestBranchSlug: a shard's name carries the branch in a form every
// filesystem accepts, and a fold with no branch still gets a name.
func TestBranchSlug(t *testing.T) {
	for branch, want := range map[string]string{
		"main":                "main",
		"feature/Some_Work":   "feature-some-work",
		"--a..b--":            "a-b",
		"release/v1.2.3":      "release-v1-2-3",
		"Zz09":                "zz09",
		"café":                "caf",
		"":                    "no-branch",
		"///":                 "no-branch",
		"a/-/b":               "a-b",
		"UPPER/lower/MiXeD-9": "upper-lower-mixed-9",
	} {
		if got := memory.BranchSlug(branch); got != want {
			t.Errorf("BranchSlug(%q) = %q, want %q", branch, got, want)
		}
	}
}

// TestReadHitRecordRejectsWhatItCannotTrust: a record is read whole or not at
// all, so a malformed one can be skipped without its reads half-counting.
func TestReadHitRecordRejectsWhatItCannotTrust(t *testing.T) {
	dir := t.TempDir()
	missing, err := memory.ReadHitRecord(filepath.Join(dir, "absent.json"))
	if err != nil || len(missing) != 0 {
		t.Errorf("missing record = %v (%v), want empty and no error", missing, err)
	}

	for name, body := range map[string]string{
		"not json":      "{",
		"wrong version": `{"version": 2, "notes": {"a": {"count": 1}}}`,
		"no version":    `{"notes": {"a": {"count": 1}}}`,
		"zero count":    `{"version": 1, "notes": {"a": {"count": 0}}}`,
		"no name":       `{"version": 1, "notes": {"": {"count": 1}}}`,
	} {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".json")
		write(t, path, body)
		if got, err := memory.ReadHitRecord(path); err == nil {
			t.Errorf("%s: read %+v, want an error", name, got)
		} else if !strings.Contains(err.Error(), path) {
			t.Errorf("%s: error %q does not name the file", name, err)
		}
	}

	ok := filepath.Join(dir, "ok.json")
	write(t, ok, `{"version": 1, "notes": {"a": {"count": 1, "first": "2026-01-01T00:00:00Z", "last": "2026-01-02T00:00:00Z"}}}`)
	got, err := memory.ReadHitRecord(ok)
	if err != nil || got["a"].Count != 1 || !got["a"].Last.Equal(day(2, 0)) {
		t.Errorf("ok record = %+v (%v)", got, err)
	}
}

// TestWriteHitRecordNeverOverwrites: a shard's name is its identity, so
// writing to a taken name fails and leaves the reads already there.
func TestWriteHitRecordNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "taken.json")
	writeRecord(t, path, memory.HitCounts{"a": {Count: 5, First: day(1, 0), Last: day(1, 0)}})

	err := memory.WriteHitRecord(path, memory.HitCounts{"b": {Count: 1}})
	if !errors.Is(err, fs.ErrExist) {
		t.Errorf("write over a taken name = %v, want fs.ErrExist", err)
	}
	got, err := memory.ReadHitRecord(path)
	if err != nil || len(got) != 1 || got["a"].Count != 5 {
		t.Errorf("taken record after a refused write = %+v (%v)", got, err)
	}
}

// TestHitCountsAdd: merging records sums the counts and keeps the widest
// window, and a record that does not know when a note was first read does not
// pull the window back to the zero time.
func TestHitCountsAdd(t *testing.T) {
	c := memory.HitCounts{
		"a": {Count: 2, First: day(3, 0), Last: day(4, 0)},
		"b": {Count: 1, Last: day(5, 0)},
	}
	c.Add(memory.HitCounts{
		"a": {Count: 3, First: day(1, 0), Last: day(2, 0)},
		"b": {Count: 4, First: day(6, 0), Last: day(6, 0)},
		"c": {Count: 1, First: day(7, 0), Last: day(7, 0)},
	})

	if h := c["a"]; h.Count != 5 || !h.First.Equal(day(1, 0)) || !h.Last.Equal(day(4, 0)) {
		t.Errorf("a = %+v, want 5 reads from day 1 to day 4", h)
	}
	if h := c["b"]; h.Count != 5 || !h.First.Equal(day(6, 0)) || !h.Last.Equal(day(6, 0)) {
		t.Errorf("b = %+v, want 5 reads with first taken from the side that knows it", h)
	}
	if h := c["c"]; h.Count != 1 || !h.First.Equal(day(7, 0)) {
		t.Errorf("c = %+v, want the one read carried over", h)
	}
	if got := c.Total(); got != 11 {
		t.Errorf("total = %d, want 11", got)
	}

	// Equal instants on both sides keep that instant.
	d := memory.HitCounts{"x": {Count: 1, First: day(2, 0), Last: day(2, 0)}}
	d.Add(memory.HitCounts{"x": {Count: 1, First: day(2, 0), Last: day(2, 0)}})
	if h := d["x"]; !h.First.Equal(day(2, 0)) || !h.Last.Equal(day(2, 0)) {
		t.Errorf("x = %+v, want its one instant kept", h)
	}
}

// TestHitShardsListsOnlyShardFiles: HitsDir can hold other files, and only a
// .json file is a shard.
func TestHitShardsListsOnlyShardFiles(t *testing.T) {
	s := stampedStore(t)
	none, err := s.HitShards()
	if err != nil || len(none) != 0 {
		t.Errorf("shards with no directory = %v (%v), want none", none, err)
	}

	write(t, filepath.Join(s.HitShardsPath(), "b.json"), "{}")
	write(t, filepath.Join(s.HitShardsPath(), "a.json"), "{}")
	write(t, filepath.Join(s.HitShardsPath(), memory.GitkeepFile), "")
	if err := os.MkdirAll(filepath.Join(s.HitShardsPath(), "dir.json"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := s.HitShards()
	if err != nil {
		t.Fatalf("shards: %v", err)
	}
	want := []string{filepath.Join(s.HitShardsPath(), "a.json"), filepath.Join(s.HitShardsPath(), "b.json")}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("shards = %v, want %v", got, want)
	}
}
