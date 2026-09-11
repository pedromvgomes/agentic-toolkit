package memory

import (
	"errors"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"
)

// Stats describes the shape of a store and whether it is paying for itself.
//
// HitRate is the fraction of notes that have been read at least once. It is
// the number the store is judged on: the index is a tax collected on every
// session, so a low hit rate means prune, never store more.
type Stats struct {
	Notes        int
	ByKind       map[Kind]int
	ByConfidence map[Confidence]int
	Anchors      int
	AnchoredFile int
	Stale        int
	Candidates   int

	// IndexBytes is the size of the generated INDEX.md, and is the tax
	// itself rather than a proxy for it: it is what an explorer loads before
	// it has decided any note is relevant. Zero when no index has been
	// generated yet.
	IndexBytes int64

	Hits     int
	NotesHit int
	HitRate  float64
	FirstHit time.Time
	LastHit  time.Time
	// Cold names the notes with no recorded hit, sorted. Where HitRate says
	// only that the store is not being repaid, this says which notes to drop,
	// which is the form of that number anyone can act on.
	//
	// Only once there are at least as many hits as notes. Below that the list
	// is non-empty by arithmetic — n reads reach at most n notes — and says
	// nothing about the notes in it. A reader of this field owes that
	// comparison against Hits and Notes before treating it as a prune list.
	Cold []string
}

// Stats computes the store's shape, including a staleness pass over the
// working tree.
func (s *Store) Stats(notes []*Note) (Stats, error) {
	st := Stats{
		Notes:        len(notes),
		ByKind:       map[Kind]int{},
		ByConfidence: map[Confidence]int{},
	}
	known := make(map[string]bool, len(notes))
	// Counted by distinct path: one file anchored by two notes, or by both a
	// concrete anchor and a glob, is still one file.
	anchored := map[string]bool{}
	for _, n := range notes {
		known[n.Name] = true
		st.ByKind[n.Kind]++
		st.ByConfidence[n.Confidence]++
		st.Anchors += len(n.Anchors)
		for _, a := range n.Anchors {
			if a.IsGlob() {
				for _, m := range a.Matches {
					anchored[m.Path] = true
				}
				continue
			}
			anchored[a.Path] = true
		}
		if s.AuditNote(n).Stale() {
			st.Stale++
		}
	}

	st.AnchoredFile = len(anchored)

	candidates, err := os.ReadDir(s.CandidatesPath())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return st, err
	}
	for _, c := range candidates {
		if !c.IsDir() && strings.HasSuffix(c.Name(), NoteExt) {
			st.Candidates++
		}
	}

	hits, err := s.Hits()
	if err != nil {
		return st, err
	}
	seen := map[string]bool{}
	for _, h := range hits {
		st.Hits++
		if st.FirstHit.IsZero() || h.At.Before(st.FirstHit) {
			st.FirstHit = h.At
		}
		if h.At.After(st.LastHit) {
			st.LastHit = h.At
		}
		// Only notes still in the store count toward the rate; a hit on a
		// pruned note says nothing about whether today's index is repaid.
		if known[h.Note] {
			seen[h.Note] = true
		}
	}
	st.NotesHit = len(seen)
	if st.Notes > 0 {
		st.HitRate = float64(st.NotesHit) / float64(st.Notes)
	}
	for _, n := range notes {
		if !seen[n.Name] {
			st.Cold = append(st.Cold, n.Name)
		}
	}
	sort.Strings(st.Cold)

	// A missing index is not an error here: `stats` is what a hook calls on a
	// store that may never have had `index` run against it, and reporting a
	// zero tax is more useful than refusing to report anything.
	if fi, err := os.Stat(s.IndexPath()); err == nil {
		st.IndexBytes = fi.Size()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return st, err
	}
	return st, nil
}
