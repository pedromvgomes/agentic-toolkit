package tests

import (
	"os"
	"path/filepath"
	"strings"
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

// TestStampRefusesASymlinkedAnchor is the escape the lexical check cannot
// see. ValidateAnchorPath reads the pattern and not the filesystem, so a path
// that spells out as project-relative still resolves wherever a link inside
// the project points — and following it records a hash of a file outside the
// repository into a note that is then committed.
func TestStampRefusesASymlinkedAnchor(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.txt")
	write(t, outside, "secrets\n")

	s := project(t, map[string]string{"internal/a/a.go": "package a\n"})
	if err := os.Symlink(outside, filepath.Join(s.ProjectRoot, "internal/a/linked.go")); err != nil {
		t.Fatal(err)
	}
	writeNote(t, s, "linked", note("linked", "  - path: internal/a/linked.go\n"))

	res, err := s.Stamp(loadOne(t, s, "linked"))
	if err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if len(res.Missing) != 1 || res.Missing[0] != "internal/a/linked.go" {
		t.Errorf("Missing = %+v, want the symlinked anchor refused", res.Missing)
	}
	if blob := loadOne(t, s, "linked").Anchors[0].Blob; blob != "" {
		t.Errorf("a symlinked anchor recorded the blob %q of a file outside the project", blob)
	}
}

// A glob never names the link, so it is the easier way in of the two: the
// pattern matches whatever the directory holds.
func TestGlobAnchorsSkipSymlinkedMatches(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.txt")
	write(t, outside, "secrets\n")

	s := project(t, map[string]string{"internal/a/real.go": "package a\n"})
	if err := os.Symlink(outside, filepath.Join(s.ProjectRoot, "internal/a/linked.go")); err != nil {
		t.Fatal(err)
	}
	writeNote(t, s, "globbed", note("globbed", "  - path: internal/a/*.go\n"))

	if _, err := s.Stamp(loadOne(t, s, "globbed")); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	for _, m := range loadOne(t, s, "globbed").Anchors[0].Matches {
		if strings.HasSuffix(m.Path, "linked.go") {
			t.Errorf("a glob followed a symlink out of the project: %+v", m)
		}
	}
}

// The escape a check on the final component cannot see. The kernel resolves
// every directory above the anchor, so a linked directory inside the project
// makes `internal/x/id_rsa` name a real regular file outside the repository —
// and hashing it writes that file's blob into a note that is then committed.
func TestStampRefusesAnAnchorUnderASymlinkedDirectory(t *testing.T) {
	outside := t.TempDir()
	write(t, filepath.Join(outside, "id_rsa"), "PRIVATE KEY\n")

	s := project(t, map[string]string{"internal/a/a.go": "package a\n"})
	if err := os.Symlink(outside, filepath.Join(s.ProjectRoot, "internal/linked")); err != nil {
		t.Fatal(err)
	}
	writeNote(t, s, "under-link", note("under-link", "  - path: internal/linked/id_rsa\n"))

	res, err := s.Stamp(loadOne(t, s, "under-link"))
	if err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if len(res.Missing) != 1 {
		t.Errorf("Missing = %+v, want the anchor under a linked directory refused", res.Missing)
	}
	if blob := loadOne(t, s, "under-link").Anchors[0].Blob; blob != "" {
		t.Errorf("recorded the blob %q of a file outside the project", blob)
	}
}

// The same directory, reached by a glob. filepath.Glob walks through the
// linked component, so the pattern enumerates the target directory and would
// record each outside filename, spelled as if it were project-relative.
func TestGlobAnchorsDoNotEnumerateASymlinkedDirectory(t *testing.T) {
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret.go"), "package secret\n")

	s := project(t, map[string]string{"internal/a/a.go": "package a\n"})
	if err := os.Symlink(outside, filepath.Join(s.ProjectRoot, "internal/linked")); err != nil {
		t.Fatal(err)
	}
	writeNote(t, s, "under-link-glob", note("under-link-glob", "  - path: internal/linked/*.go\n"))

	if _, err := s.Stamp(loadOne(t, s, "under-link-glob")); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if matches := loadOne(t, s, "under-link-glob").Anchors[0].Matches; len(matches) != 0 {
		t.Errorf("a glob enumerated a directory outside the project: %+v", matches)
	}
}

// Audit reads with HashFile, which is an os.ReadFile and resolves every path
// component. Stamp refusing the anchor leaves its recorded blob empty, so an
// audit that did not check containment would read the outside file on every
// run rather than once.
func TestAuditRefusesAnAnchorUnderASymlinkedDirectory(t *testing.T) {
	outside := t.TempDir()
	write(t, filepath.Join(outside, "id_rsa"), "PRIVATE KEY\n")

	s := project(t, map[string]string{"internal/a/a.go": "package a\n"})
	if err := os.Symlink(outside, filepath.Join(s.ProjectRoot, "internal/linked")); err != nil {
		t.Fatal(err)
	}
	writeNote(t, s, "audited", note("audited", "  - path: internal/linked/id_rsa\n"))

	drifts := s.AuditNote(loadOne(t, s, "audited")).Drifts
	if len(drifts) != 1 {
		t.Fatalf("drifts = %+v, want one refusal", drifts)
	}
	if drifts[0].Kind != memory.DriftInvalid {
		t.Errorf("drift kind = %q, want invalid", drifts[0].Kind)
	}
	if drifts[0].Now != "" {
		t.Errorf("audit reported the blob %q of a file outside the project", drifts[0].Now)
	}
}

// A deleted anchor is missing, not invalid. The distinction carries: missing
// is the one an agent may act on by dropping the anchor, and a containment
// check that cannot resolve an absent path would call every deletion an
// escape.
func TestADeletedAnchorIsMissingRatherThanOutsideTheProject(t *testing.T) {
	s := stampedStore(t)
	if err := os.Remove(filepath.Join(s.ProjectRoot, "internal/resolver/graph.go")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	for _, d := range s.AuditNote(loadOne(t, s, "pins-shas")).Drifts {
		if d.Path == "internal/resolver/graph.go" && d.Kind != memory.DriftMissing {
			t.Errorf("a deleted anchor reported as %q: %+v", d.Kind, d)
		}
	}
}

// A symlink whose target is gone is a missing anchor, not an escape. Lstat
// succeeds on it and EvalSymlinks does not, so the two failures look alike
// from the wrong side of the link.
func TestADanglingSymlinkedAnchorIsMissingRatherThanOutsideTheProject(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone.go")
	s := project(t, map[string]string{"internal/a/a.go": "package a\n"})
	if err := os.Symlink(gone, filepath.Join(s.ProjectRoot, "internal/a/dangling.go")); err != nil {
		t.Fatal(err)
	}
	writeNote(t, s, "dangling", note("dangling", "  - path: internal/a/dangling.go\n    blob: 0123456789ab\n"))

	drifts := s.AuditNote(loadOne(t, s, "dangling")).Drifts
	if len(drifts) != 1 {
		t.Fatalf("drifts = %+v, want one", drifts)
	}
	if drifts[0].Kind != memory.DriftMissing {
		t.Errorf("a dangling symlink reported as %q, want missing: %+v", drifts[0].Kind, drifts[0])
	}
}

// Replacing an anchored file with an in-project symlink to identical content
// must not audit as fresh: `anchor` refuses to stamp such a path, so a note
// reading as held against it would be held against a file the store will not
// record.
func TestAuditRefusesAnAnchorReplacedByASymlink(t *testing.T) {
	s := stampedStore(t)
	target := filepath.Join(s.ProjectRoot, "internal/resolver/graph.go")
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(s.ProjectRoot, "internal/resolver/graph_copy.go")
	write(t, copyPath, string(body))
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(copyPath, target); err != nil {
		t.Fatal(err)
	}

	for _, d := range s.AuditNote(loadOne(t, s, "pins-shas")).Drifts {
		if d.Path == "internal/resolver/graph.go" {
			if d.Kind != memory.DriftInvalid {
				t.Errorf("a symlinked anchor audited as %q, want invalid: %+v", d.Kind, d)
			}
			return
		}
	}
	t.Error("replacing the anchored file with a symlink audited as fresh")
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

// TestStampClearsBlobOnAGlobThatMatchesNothing: a `blob` left over from
// when the path was concrete is meaningless on a glob anchor, and lint
// rejects it — with no command able to clear it if Stamp does not.
func TestStampClearsBlobOnAGlobThatMatchesNothing(t *testing.T) {
	s := project(t, map[string]string{"internal/resolver/graph.go": "package resolver\n"})
	writeNote(t, s, "switched", note("switched", "  - path: internal/resolver/graph.go\n"))
	if _, err := s.Stamp(loadOne(t, s, "switched")); err != nil {
		t.Fatalf("stamp: %v", err)
	}

	// The curator repoints the anchor at a pattern that matches nothing yet.
	n := loadOne(t, s, "switched")
	n.Anchors[0].Path = "internal/nothing/*.go"
	raw, err := n.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	write(t, n.File, string(raw))

	if _, err := s.Stamp(loadOne(t, s, "switched")); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if blob := loadOne(t, s, "switched").Anchors[0].Blob; blob != "" {
		t.Errorf("glob anchor kept blob %q; lint would reject it with no way to fix", blob)
	}
}
