package memory

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The committed half of the hit record. The local log (HitsFile) is
// gitignored, so on its own it describes one working copy and a fresh clone
// reports zero reads; folding it into a shard is what lets the record travel
// with the branch.
const (
	// HitsDir holds one shard per fold. Every fold writes a file no other
	// fold names, so two branches that both fold merge without a conflict —
	// a single committed counter would conflict on every merge.
	HitsDir = "hits"

	// CompactedHitsFile is the shards' union, in the same shape as a shard.
	CompactedHitsFile = "hits.json"

	// HitRecordVersion is the `version` a shard and the compacted file
	// carry. A record with any other version is unreadable, not guessed at.
	HitRecordVersion = 1

	// shardExt is the extension a file in HitsDir needs to be read as a
	// shard; anything else there is ignored.
	shardExt = ".json"

	// noBranchSlug names the shard of a fold made where no branch is checked
	// out: a detached HEAD, or a store outside any git repository.
	noBranchSlug = "no-branch"

	// maxShardAttempts bounds how many names a fold tries before giving up.
	// A collision needs the same day, branch and random suffix, so a second
	// attempt already means something other than chance is repeating it.
	maxShardAttempts = 8
)

// Hit is one read of a note through `agtk memory show`.
//
// Accounting rides on the read path rather than on a separate "record a
// hit" call, because a separate call is exactly what an agent skips under
// context pressure — and a denominator that drifts makes the hit rate lie
// in the reassuring direction.
type Hit struct {
	Note string    `json:"note"`
	At   time.Time `json:"at"`
}

// NoteHits is one note's reads in a hit record: how many, and the first and
// last of them.
type NoteHits struct {
	Count int       `json:"count"`
	First time.Time `json:"first"`
	Last  time.Time `json:"last"`
}

// HitCounts maps a note name to its reads. It is the per-note body of a
// shard and of the compacted file, and what the local log folds into.
type HitCounts map[string]NoteHits

// hitRecord is the on-disk form of a shard and of the compacted file:
//
//	{
//	  "version": 1,
//	  "notes": {
//	    "<note-name>": {"count": 3, "first": "<RFC 3339>", "last": "<RFC 3339>"}
//	  }
//	}
//
// Notes marshals with its keys sorted, so the same counts always produce the
// same bytes.
type hitRecord struct {
	Version int       `json:"version"`
	Notes   HitCounts `json:"notes"`
}

// Add merges other into c: counts are summed, first is the earliest and last
// the latest. A zero time on either side is "unknown", never the earliest.
func (c HitCounts) Add(other HitCounts) {
	for name, h := range other {
		c[name] = mergeNoteHits(c[name], h)
	}
}

// Total is the number of reads across every note.
func (c HitCounts) Total() int {
	n := 0
	for _, h := range c {
		n += h.Count
	}
	return n
}

func mergeNoteHits(a, b NoteHits) NoteHits {
	out := NoteHits{Count: a.Count + b.Count, First: a.First, Last: a.Last}
	if out.First.IsZero() || (!b.First.IsZero() && b.First.Before(out.First)) {
		out.First = b.First
	}
	if b.Last.After(out.Last) {
		out.Last = b.Last
	}
	return out
}

// TallyHits folds individual reads into per-note counts.
func TallyHits(hits []Hit) HitCounts {
	c := HitCounts{}
	for _, h := range hits {
		at := h.At.UTC()
		c[h.Note] = mergeNoteHits(c[h.Note], NoteHits{Count: 1, First: at, Last: at})
	}
	return c
}

// HitShardsPath and CompactedHitsPath locate the committed hit record.
func (s *Store) HitShardsPath() string     { return filepath.Join(s.Root, HitsDir) }
func (s *Store) CompactedHitsPath() string { return filepath.Join(s.Root, CompactedHitsFile) }

// RecordHit appends one line to the gitignored hits log. Failures are the
// caller's to ignore: telemetry must never break a read.
func (s *Store) RecordHit(name string, at time.Time) error {
	// `show` can be the first command ever run against a hand-created
	// store, and writing the log without its .gitignore would commit local
	// telemetry on the next `git add -A`.
	if err := s.ensureGitignore(); err != nil {
		return err
	}
	f, err := os.OpenFile(s.HitsPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) // #nosec G302,G304 -- local, gitignored telemetry in the store the invoker named
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	line, err := json.Marshal(Hit{Note: name, At: at.UTC()})
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Close()
}

// Hits reads the log. A malformed line is skipped rather than fatal — the
// file is append-only local telemetry and a torn write must not take out
// `stats`.
func (s *Store) Hits() ([]Hit, error) {
	f, err := os.Open(s.HitsPath()) // #nosec G304 -- reads the store's own hits log
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.HitsPath(), err)
	}
	defer func() { _ = f.Close() }()

	var hits []Hit
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var h Hit
		if err := json.Unmarshal(sc.Bytes(), &h); err != nil {
			continue
		}
		hits = append(hits, h)
	}
	if err := sc.Err(); err != nil {
		return hits, fmt.Errorf("scan %s: %w", s.HitsPath(), err)
	}
	return hits, nil
}

// ReadHitRecord reads a shard or the compacted file. A missing file is an
// empty record, not an error. A file that does not parse, carries another
// version, or holds a count below one is an error naming the file; callers
// that read many records skip it rather than fail.
func ReadHitRecord(path string) (HitCounts, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- reads a hit record inside the store the invoker named
	if errors.Is(err, fs.ErrNotExist) {
		return HitCounts{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var rec hitRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if rec.Version != HitRecordVersion {
		return nil, fmt.Errorf("%s: version %d, want %d", path, rec.Version, HitRecordVersion)
	}
	out := make(HitCounts, len(rec.Notes))
	for name, h := range rec.Notes {
		if name == "" {
			return nil, fmt.Errorf("%s: a note with no name", path)
		}
		if h.Count < 1 {
			return nil, fmt.Errorf("%s: note %q has count %d, want at least 1", path, name, h.Count)
		}
		out[name] = NoteHits{Count: h.Count, First: h.First.UTC(), Last: h.Last.UTC()}
	}
	return out, nil
}

// WriteHitRecord writes counts to path, which must not exist yet. Refusing
// to overwrite is what makes a shard's name its identity: a fold that picked
// a taken name fails here instead of discarding the reads already in it. A
// write that fails part-way removes the partial file.
func WriteHitRecord(path string, counts HitCounts) error {
	if counts == nil {
		counts = HitCounts{}
	}
	raw, err := json.MarshalIndent(hitRecord{Version: HitRecordVersion, Notes: counts}, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644) // #nosec G302,G304 -- committed hit record in the store the invoker named
	if err != nil {
		return err
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// HitShards lists the shard files in HitsDir, sorted by name. A missing
// directory is no shards.
func (s *Store) HitShards() ([]string, error) {
	entries, err := os.ReadDir(s.HitShardsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.HitShardsPath(), err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), shardExt) {
			continue
		}
		out = append(out, filepath.Join(s.HitShardsPath(), e.Name()))
	}
	sort.Strings(out)
	return out, nil
}

// SharedHits is the committed hit record: the compacted file and every
// shard, merged.
type SharedHits struct {
	Counts HitCounts
	// Shards is how many shard files were read into Counts.
	Shards int
	// Compacted reports that the compacted file exists and was read.
	Compacted bool
	// Skipped names each record that could not be read. Its reads are left
	// out of Counts; one bad file must not take out `stats`.
	Skipped []error
}

// Present reports whether any committed record contributed, which is what
// decides whether a hit count describes more than this checkout.
func (h SharedHits) Present() bool { return h.Shards > 0 || h.Compacted }

// SharedHits reads the committed hit record. Only a HitsDir that exists and
// cannot be listed is an error; an unreadable record is reported in Skipped.
func (s *Store) SharedHits() (SharedHits, error) {
	out := SharedHits{Counts: HitCounts{}}
	switch _, err := os.Stat(s.CompactedHitsPath()); {
	case err == nil:
		c, err := ReadHitRecord(s.CompactedHitsPath())
		if err != nil {
			out.Skipped = append(out.Skipped, err)
		} else {
			out.Counts.Add(c)
			out.Compacted = true
		}
	case !errors.Is(err, fs.ErrNotExist):
		out.Skipped = append(out.Skipped, err)
	}

	shards, err := s.HitShards()
	if err != nil {
		return out, err
	}
	for _, p := range shards {
		c, err := ReadHitRecord(p)
		if err != nil {
			out.Skipped = append(out.Skipped, err)
			continue
		}
		out.Counts.Add(c)
		out.Shards++
	}
	return out, nil
}

// FoldOptions is what a fold's shard is named from.
type FoldOptions struct {
	// Now dates the shard, in UTC.
	Now time.Time
	// Branch is the checked-out branch; empty when there is none.
	Branch string
	// Suffix returns the random part of the shard's name. Nil draws it from
	// crypto/rand.
	Suffix func() string
}

// Fold is what FoldHits did.
type Fold struct {
	// Shard is the file written; empty when the log held nothing to fold.
	Shard string
	// Hits is the number of reads folded, and Notes the distinct notes they
	// were of.
	Hits  int
	Notes int
}

// FoldHits writes the local log's reads to a shard of their own and then
// empties the log. The order is what keeps a read from being lost: a shard
// that cannot be written leaves the log untouched, and a log that cannot be
// emptied has its shard removed again so no read is counted twice.
//
// A read recorded by `show` between this reading the log and emptying it is
// dropped with the rest of the log.
//
// A log with no readable reads writes nothing and leaves the log as it is.
// Nothing is committed: the shard is left for the caller's own commit.
func (s *Store) FoldHits(opts FoldOptions) (Fold, error) {
	hits, err := s.Hits()
	if err != nil {
		return Fold{}, err
	}
	if len(hits) == 0 {
		return Fold{}, nil
	}
	counts := TallyHits(hits)

	if err := os.MkdirAll(s.HitShardsPath(), 0o750); err != nil {
		return Fold{}, fmt.Errorf("create %s: %w", s.HitShardsPath(), err)
	}
	suffix := opts.Suffix
	if suffix == nil {
		suffix = randomSuffix
	}
	var shard string
	for attempt := 0; ; attempt++ {
		if attempt == maxShardAttempts {
			return Fold{}, fmt.Errorf("no free shard name in %s after %d attempts", s.HitShardsPath(), maxShardAttempts)
		}
		shard = filepath.Join(s.HitShardsPath(), ShardName(opts.Now, opts.Branch, suffix()))
		err := WriteHitRecord(shard, counts)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) {
			return Fold{}, err
		}
	}

	if err := os.Truncate(s.HitsPath(), 0); err != nil {
		if rmErr := os.Remove(shard); rmErr != nil {
			return Fold{}, fmt.Errorf("empty %s: %w; the reads are now in both it and %s, so remove that shard by hand", s.HitsPath(), err, shard)
		}
		return Fold{}, fmt.Errorf("empty %s: %w", s.HitsPath(), err)
	}
	return Fold{Shard: shard, Hits: len(hits), Notes: len(counts)}, nil
}

// ShardName is a shard's file name: the UTC date, the branch slug and a
// suffix, as `<YYYY-MM-DD>-<branch-slug>-<suffix>.json`. The date and branch
// make HitsDir readable at a glance; the suffix is what keeps two folds on
// the same day and branch — in one clone or in two — from naming the same
// file.
func ShardName(now time.Time, branch, suffix string) string {
	return now.UTC().Format("2006-01-02") + "-" + BranchSlug(branch) + "-" + suffix + shardExt
}

// BranchSlug lower-cases branch and turns every run of characters outside
// [a-z0-9] into a single `-`, trimmed from both ends. A branch with nothing
// left, or no branch at all, is noBranchSlug.
func BranchSlug(branch string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(branch) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.TrimSuffix(b.String(), "-")
	if slug == "" {
		return noBranchSlug
	}
	return slug
}

func randomSuffix() string {
	var buf [4]byte
	// crypto/rand.Read never returns an error: a platform that cannot supply
	// randomness aborts the process instead.
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}
