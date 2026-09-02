package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

const twoAnchors = "  - path: internal/resolver/graph.go\n  - path: internal/lockfile/*.go\n"

func stampedStore(t *testing.T) *memory.Store {
	t.Helper()
	s := project(t, map[string]string{
		"internal/resolver/graph.go":  "package resolver\n",
		"internal/lockfile/types.go":  "package lockfile\n",
		"internal/lockfile/parser.go": "package lockfile\n",
	})
	writeNote(t, s, "pins-shas", note("pins-shas", twoAnchors))
	if _, err := s.Stamp(loadOne(t, s, "pins-shas")); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	return s
}

// TestStampRecordsHashesAndExpandsGlobs: after stamping, a concrete anchor
// carries a blob and a glob anchor carries one match per file.
func TestStampRecordsHashesAndExpandsGlobs(t *testing.T) {
	s := stampedStore(t)
	n := loadOne(t, s, "pins-shas")

	if got := n.Anchors[0].Blob; got != memory.BlobHash([]byte("package resolver\n")) {
		t.Errorf("concrete anchor blob = %q", got)
	}
	if len(n.Anchors[0].Matches) != 0 {
		t.Errorf("concrete anchor should not carry matches: %+v", n.Anchors[0].Matches)
	}
	if got := len(n.Anchors[1].Matches); got != 2 {
		t.Fatalf("glob expanded to %d matches, want 2: %+v", got, n.Anchors[1].Matches)
	}
	if n.Anchors[1].Blob != "" {
		t.Error("glob anchor should not carry a single blob")
	}
	// Matches are sorted so a re-stamp on another machine produces no diff.
	if n.Anchors[1].Matches[0].Path != "internal/lockfile/parser.go" {
		t.Errorf("matches not sorted: %+v", n.Anchors[1].Matches)
	}
}

// TestStampIsIdempotent: re-stamping an unchanged tree must not rewrite the
// file, or every hook run would leave a diff behind.
func TestStampIsIdempotent(t *testing.T) {
	s := stampedStore(t)
	path := filepath.Join(s.NotesPath(), "pins-shas.md")
	before := read(t, path)

	res, err := s.Stamp(loadOne(t, s, "pins-shas"))
	if err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if res.Changed {
		t.Error("second stamp reported a change")
	}
	if after := read(t, path); after != before {
		t.Errorf("second stamp rewrote the note:\n%s", after)
	}
}

// TestStampReportsMissingAnchor: a path that does not exist is recorded as
// unstamped rather than failing the run, so one bad anchor cannot block
// stamping the rest of the store.
func TestStampReportsMissingAnchor(t *testing.T) {
	s := project(t, nil)
	writeNote(t, s, "gone", note("gone", "  - path: internal/gone.go\n"))

	res, err := s.Stamp(loadOne(t, s, "gone"))
	if err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if len(res.Missing) != 1 || res.Missing[0] != "internal/gone.go" {
		t.Errorf("Missing = %+v, want [internal/gone.go]", res.Missing)
	}
}

// TestAuditCleanTree: nothing drifted, so nothing is reported.
func TestAuditCleanTree(t *testing.T) {
	s := stampedStore(t)

	if a := s.AuditNote(loadOne(t, s, "pins-shas")); a.Stale() {
		t.Errorf("clean tree reported stale: %+v", a.Drifts)
	}
}

// TestAuditNamesEveryKindOfDrift: the point of expanding globs at stamp
// time is that audit can name the file that moved, not just the pattern.
func TestAuditNamesEveryKindOfDrift(t *testing.T) {
	s := stampedStore(t)
	write(t, filepath.Join(s.ProjectRoot, "internal/resolver/graph.go"), "package resolver // changed\n")
	write(t, filepath.Join(s.ProjectRoot, "internal/lockfile/errors.go"), "package lockfile\n")
	if err := os.Remove(filepath.Join(s.ProjectRoot, "internal/lockfile/parser.go")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	got := map[memory.DriftKind]string{}
	for _, d := range s.AuditNote(loadOne(t, s, "pins-shas")).Drifts {
		got[d.Kind] = d.Path
	}
	want := map[memory.DriftKind]string{
		memory.DriftChanged: "internal/resolver/graph.go",
		memory.DriftAdded:   "internal/lockfile/errors.go",
		memory.DriftRemoved: "internal/lockfile/parser.go",
	}
	for kind, path := range want {
		if got[kind] != path {
			t.Errorf("drift %s = %q, want %q (all drifts: %+v)", kind, got[kind], path, got)
		}
	}
}

// TestAuditReportsDeletedAnchor: a concrete anchor whose file is gone is
// missing, which is a stronger signal than changed.
func TestAuditReportsDeletedAnchor(t *testing.T) {
	s := stampedStore(t)
	if err := os.Remove(filepath.Join(s.ProjectRoot, "internal/resolver/graph.go")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	drifts := s.AuditNote(loadOne(t, s, "pins-shas")).Drifts
	if len(drifts) == 0 || drifts[0].Kind != memory.DriftMissing {
		t.Fatalf("drifts = %+v, want a missing drift", drifts)
	}
}

// TestAuditWritesNothing is the guarantee that makes audit hook-safe.
func TestAuditWritesNothing(t *testing.T) {
	s := stampedStore(t)
	path := filepath.Join(s.NotesPath(), "pins-shas.md")
	before := read(t, path)
	write(t, filepath.Join(s.ProjectRoot, "internal/resolver/graph.go"), "package resolver // changed\n")

	notes, _ := s.LoadNotes()
	if audits := s.Audit(notes); len(audits) != 1 || !audits[0].Stale() {
		t.Fatalf("expected one stale note, got %+v", audits)
	}
	if after := read(t, path); after != before {
		t.Error("audit rewrote the note; staleness must stay derived")
	}
}

// TestStampSkipsDirectoryAnchor: anchoring a directory by mistake is a
// miss, not a hard failure — one bad anchor must not abort the run and
// leave the rest of the store unstamped.
func TestStampSkipsDirectoryAnchor(t *testing.T) {
	s := stampedStore(t)
	writeNote(t, s, "dir-anchor", note("dir-anchor", "  - path: internal/resolver\n"))

	res, err := s.Stamp(loadOne(t, s, "dir-anchor"))
	if err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if len(res.Missing) != 1 || res.Missing[0] != "internal/resolver" {
		t.Errorf("Missing = %+v, want the directory anchor", res.Missing)
	}
}

// TestStampRejectsDoublestar: filepath.Glob expands `**` as one directory
// level, which would leave a note looking anchored while never noticing a
// file added deeper down.
func TestStampRejectsDoublestar(t *testing.T) {
	s := project(t, map[string]string{"internal/a/a.go": "package a\n"})
	writeNote(t, s, "deep", note("deep", "  - path: internal/**/*.go\n"))

	if _, err := s.Stamp(loadOne(t, s, "deep")); err == nil {
		t.Fatal("expected an error for a `**` anchor")
	}
}

// TestStampKeepsHashesWhenAnchorGoesMissing is the guard against a note
// silently re-baselining: if the anchored file is away when a hook stamps
// the store, dropping the recorded hash would make the note read as fresh
// once a *different* file appeared at that path.
func TestStampKeepsHashesWhenAnchorGoesMissing(t *testing.T) {
	s := stampedStore(t)
	before := loadOne(t, s, "pins-shas")
	wasBlob := before.Anchors[0].Blob
	wasMatches := len(before.Anchors[1].Matches)

	if err := os.Remove(filepath.Join(s.ProjectRoot, "internal/resolver/graph.go")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(s.ProjectRoot, "internal/lockfile")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := s.Stamp(loadOne(t, s, "pins-shas")); err != nil {
		t.Fatalf("stamp: %v", err)
	}

	after := loadOne(t, s, "pins-shas")
	if after.Anchors[0].Blob != wasBlob {
		t.Errorf("concrete anchor blob = %q, want the recorded %q kept", after.Anchors[0].Blob, wasBlob)
	}
	if len(after.Anchors[1].Matches) != wasMatches {
		t.Errorf("glob matches = %d, want the recorded %d kept", len(after.Anchors[1].Matches), wasMatches)
	}

	// The file coming back with different content must still read as drift.
	write(t, filepath.Join(s.ProjectRoot, "internal/resolver/graph.go"), "package resolver // rewritten\n")
	if !s.AuditNote(loadOne(t, s, "pins-shas")).Stale() {
		t.Error("note read as fresh after its anchored file was replaced")
	}
}

// TestStampRejectsEscapingAnchor: anchors are project-relative by
// contract, and a `..` path would record hashes — and, for a glob, file
// names — from outside the repo into a committed note.
func TestStampRejectsEscapingAnchor(t *testing.T) {
	s := project(t, nil)
	for name, path := range map[string]string{
		"parent":       "../outside.txt",
		"parent glob":  "../*",
		"absolute":     "/etc/hosts",
		"sneaky climb": "internal/../../outside.txt",
	} {
		t.Run(name, func(t *testing.T) {
			writeNote(t, s, "escape", note("escape", "  - path: "+path+"\n"))
			if _, err := s.Stamp(loadOne(t, s, "escape")); err == nil {
				t.Errorf("stamped an anchor that escapes the project root: %s", path)
			}
		})
	}
}

// TestStampMarksMissingAnchors: the kept hash must be distinguishable from
// a freshly computed one, or a report of a deleted file reads as a success.
func TestStampMarksMissingAnchors(t *testing.T) {
	s := stampedStore(t)
	if err := os.Remove(filepath.Join(s.ProjectRoot, "internal/resolver/graph.go")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	res, err := s.Stamp(loadOne(t, s, "pins-shas"))
	if err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if !res.Anchors[0].Missing {
		t.Errorf("anchor %+v not marked missing", res.Anchors[0])
	}
	if res.Anchors[1].Missing {
		t.Errorf("healthy glob anchor marked missing: %+v", res.Anchors[1])
	}
}
