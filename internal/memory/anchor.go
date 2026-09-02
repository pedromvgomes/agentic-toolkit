package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// StampResult reports what `agtk memory anchor` did to one note.
type StampResult struct {
	Name string
	// Changed is false when every recorded hash already matched, in which
	// case the file is left untouched so stamping produces no diff noise.
	Changed bool
	// Anchors mirrors the note's anchors after stamping, for reporting.
	Anchors []StampedAnchor
	// Missing lists concrete anchor paths that do not exist and glob
	// patterns that expanded to nothing. Stamping does not fail on them —
	// the note keeps both the anchor and its previously recorded hashes, so
	// audit keeps reporting the drift — but the caller surfaces them.
	Missing []string
}

// StampedAnchor is one anchor's post-stamp state.
type StampedAnchor struct {
	Path    string
	Blob    string
	Matches int
	IsGlob  bool
	// Missing marks an anchor whose path resolved to nothing. Its Blob and
	// Matches are then the values kept from the previous stamp, so without
	// this flag a report cannot tell a fresh hash from a preserved one.
	Missing bool
}

// Stamp recomputes n's anchors from the working tree: concrete paths get
// their current blob, globs are expanded to their current matches. It
// writes the note only when something changed.
//
// This is the only sanctioned write to notes/. It exists so a curator never
// has to produce a hash itself, and so a hand-written seed note can be
// anchored with one command.
func (s *Store) Stamp(n *Note) (StampResult, error) {
	res := StampResult{Name: n.Name}

	before, err := n.Render()
	if err != nil {
		return res, err
	}

	for i := range n.Anchors {
		a := &n.Anchors[i]
		if err := ValidateAnchorPath(a.Path); err != nil {
			return res, err
		}
		switch {
		case a.IsGlob():
			matches, err := s.expandGlob(a.Path)
			if err != nil {
				return res, err
			}
			if len(matches) == 0 {
				// Keep what was recorded. Clearing it would turn a real
				// signal into silence: the pattern would look never-stamped,
				// and if the files came back changed a later stamp would
				// record the new content as though it had always been so.
				// Matches are preserved, but a `blob` is meaningless on a
				// glob anchor: left behind from when the path was concrete,
				// it would trip lint with no command able to clear it.
				res.Missing = append(res.Missing, a.Path)
				a.Blob = ""
				res.Anchors = append(res.Anchors, StampedAnchor{
					Path: a.Path, Matches: len(a.Matches), IsGlob: true, Missing: true,
				})
				continue
			}
			a.Blob = ""
			a.Matches = matches
			res.Anchors = append(res.Anchors, StampedAnchor{Path: a.Path, Matches: len(matches), IsGlob: true})
		default:
			blob, err := hashRegularFile(s.abs(a.Path))
			if err != nil {
				return res, fmt.Errorf("hash %s: %w", a.Path, err)
			}
			if blob == "" {
				// Absent, or a directory anchored by mistake. Same reasoning
				// as the glob case: the recorded hash stays, so audit keeps
				// reporting the file as missing instead of the note quietly
				// re-baselining onto whatever appears there next.
				res.Missing = append(res.Missing, a.Path)
				res.Anchors = append(res.Anchors, StampedAnchor{Path: a.Path, Blob: a.Blob, Missing: true})
				continue
			}
			a.Blob = blob
			a.Matches = nil
			res.Anchors = append(res.Anchors, StampedAnchor{Path: a.Path, Blob: blob})
		}
	}

	after, err := n.Render()
	if err != nil {
		return res, err
	}
	if string(before) == string(after) {
		return res, nil
	}
	if err := os.WriteFile(n.File, after, 0o644); err != nil { // #nosec G306 -- notes are committed source, world-readable by design
		return res, fmt.Errorf("write %s: %w", n.File, err)
	}
	res.Changed = true
	return res, nil
}

// hashRegularFile returns the blob id of a regular file, or "" when the
// path is absent or is not a regular file. Only a genuine read failure is
// an error.
func hashRegularFile(abs string) (string, error) {
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", nil
	}
	return HashFile(abs)
}

// expandGlob resolves a project-relative pattern to its matching files,
// sorted, with each file's current blob. Directories are skipped so a
// pattern like `internal/*` anchors to files rather than to directory
// entries that can never be hashed.
func (s *Store) expandGlob(pattern string) ([]Match, error) {
	if err := ValidateAnchorPath(pattern); err != nil {
		return nil, err
	}
	hits, err := filepath.Glob(s.abs(pattern))
	if err != nil {
		return nil, fmt.Errorf("bad glob %q: %w", pattern, err)
	}
	var matches []Match
	for _, hit := range hits {
		info, err := os.Stat(hit)
		if err != nil || info.IsDir() {
			continue
		}
		blob, err := HashFile(hit)
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", hit, err)
		}
		matches = append(matches, Match{Path: s.rel(hit), Blob: blob})
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Path < matches[j].Path })
	return matches, nil
}
