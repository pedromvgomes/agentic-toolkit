package memory

import (
	"os"
	"sort"
)

// DriftKind names how one anchored file diverged from what was stamped.
type DriftKind string

const (
	// DriftChanged: the file exists but no longer hashes to the recorded blob.
	DriftChanged DriftKind = "changed"
	// DriftMissing: the anchored file is gone, or the anchor was never stamped.
	DriftMissing DriftKind = "missing"
	// DriftAdded: a new file matches a glob anchor that did not match it before.
	DriftAdded DriftKind = "added"
	// DriftRemoved: a file that matched a glob anchor no longer does.
	DriftRemoved DriftKind = "removed"
	// DriftInvalid: the anchor could not be evaluated at all — an
	// unsupported pattern, or a path that escapes the project.
	DriftInvalid DriftKind = "invalid"
	// DriftUnstamped: the anchored file is present but carries no recorded
	// hash. Distinct from DriftMissing, which an agent may reasonably act on
	// by dropping the anchor; this one is fixed by running `anchor`.
	DriftUnstamped DriftKind = "unstamped"
)

// Drift is one divergence between a note's anchors and the working tree.
type Drift struct {
	Kind    DriftKind
	Path    string
	Pattern string // set when the drift came from a glob anchor
	Was     string // recorded blob; only ever a hash
	Now     string // current blob; only ever a hash
	Detail  string // why the anchor could not be evaluated (DriftInvalid)
}

// NoteAudit is the staleness verdict for one note.
type NoteAudit struct {
	Name   string
	Drifts []Drift
}

// Stale reports whether anything diverged.
func (a NoteAudit) Stale() bool { return len(a.Drifts) > 0 }

// Audit compares every note's anchors against the working tree. It is a
// pure read: staleness is derived on every run rather than stored, so it
// cannot go out of date, and running it from a hook leaves no diff behind.
func (s *Store) Audit(notes []*Note) []NoteAudit {
	out := make([]NoteAudit, 0, len(notes))
	for _, n := range notes {
		out = append(out, NoteAudit{Name: n.Name, Drifts: s.auditNote(n)})
	}
	return out
}

// AuditNote is Audit for a single note.
func (s *Store) AuditNote(n *Note) NoteAudit {
	return NoteAudit{Name: n.Name, Drifts: s.auditNote(n)}
}

func (s *Store) auditNote(n *Note) []Drift {
	var drifts []Drift
	for _, a := range n.Anchors {
		if err := ValidateAnchorPath(a.Path); err != nil {
			drifts = append(drifts, Drift{Kind: DriftInvalid, Path: a.Path, Detail: err.Error()})
			continue
		}
		if a.IsGlob() {
			drifts = append(drifts, s.auditGlob(a)...)
			continue
		}
		now, err := HashFile(s.abs(a.Path))
		switch {
		case os.IsNotExist(err):
			drifts = append(drifts, Drift{Kind: DriftMissing, Path: a.Path, Was: a.Blob})
		case err != nil:
			drifts = append(drifts, Drift{Kind: DriftInvalid, Path: a.Path, Was: a.Blob, Detail: err.Error()})
		case a.Blob == "":
			drifts = append(drifts, Drift{Kind: DriftUnstamped, Path: a.Path, Now: now})
		case now != a.Blob:
			drifts = append(drifts, Drift{Kind: DriftChanged, Path: a.Path, Was: a.Blob, Now: now})
		}
	}
	sort.SliceStable(drifts, func(i, j int) bool { return drifts[i].Path < drifts[j].Path })
	return drifts
}

// auditGlob set-differences the stamped matches against the current
// expansion, so an added or removed file is named rather than merely
// implied. A note most often stops holding because something new appeared.
func (s *Store) auditGlob(a Anchor) []Drift {
	current, err := s.expandGlob(a.Path)
	if err != nil {
		return []Drift{{Kind: DriftInvalid, Pattern: a.Path, Path: a.Path, Detail: err.Error()}}
	}
	was := make(map[string]string, len(a.Matches))
	for _, m := range a.Matches {
		was[m.Path] = m.Blob
	}

	var drifts []Drift
	for _, m := range current {
		prev, ok := was[m.Path]
		switch {
		case !ok:
			drifts = append(drifts, Drift{Kind: DriftAdded, Path: m.Path, Pattern: a.Path, Now: m.Blob})
		case prev != m.Blob:
			drifts = append(drifts, Drift{Kind: DriftChanged, Path: m.Path, Pattern: a.Path, Was: prev, Now: m.Blob})
		}
		delete(was, m.Path)
	}
	for path, blob := range was {
		drifts = append(drifts, Drift{Kind: DriftRemoved, Path: path, Pattern: a.Path, Was: blob})
	}
	return drifts
}
