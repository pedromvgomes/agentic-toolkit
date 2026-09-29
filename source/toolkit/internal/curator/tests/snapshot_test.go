package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-driver/agentictest"

	"github.com/pedromvgomes/agentic-toolkit/internal/curator"
)

// runOver runs the curator, unconfined by any fixture child, over p's store as
// it stands, returning the fake so a test can say whether the child started.
func runOver(t *testing.T, p *project) (*agentictest.Fake, error) {
	t.Helper()
	fake := (&agentictest.Fake{Stdout: emptyReportEnvelope}).Build(t)
	_, err := curator.Run(t.Context(), curator.Options{
		Provider:      "claudecode",
		Binary:        fake.Path(),
		WorkDir:       p.root,
		StoreRoot:     p.store.Root,
		NotesDir:      p.store.NotesPath(),
		CandidatesDir: p.store.CandidatesPath(),
	})
	return fake, err
}

// replaceWithFile turns a directory into a regular file of the same name, so
// reading it as a directory fails with something other than "not there".
func replaceWithFile(t *testing.T, dir string) {
	t.Helper()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove %s: %v", dir, err)
	}
	writeFile(t, dir, "not a directory\n")
}

// A run that cannot say what the store held before it started has nothing to
// diff afterwards, so it is refused rather than started blind.
func TestARunOverAnUnreadableStoreNeverStarts(t *testing.T) {
	for name, unreadable := range map[string]func(*testing.T, *project){
		"notes is a file":      func(t *testing.T, p *project) { replaceWithFile(t, p.store.NotesPath()) },
		"candidates is a file": func(t *testing.T, p *project) { replaceWithFile(t, p.store.CandidatesPath()) },
		"a note is a dangling symlink": func(t *testing.T, p *project) {
			if err := os.Symlink(filepath.Join(p.root, "absent.md"), p.notePath("dangling")); err != nil {
				t.Fatalf("symlink: %v", err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProject(t)
			unreadable(t, p)

			fake, err := runOver(t, p)
			if err == nil {
				t.Fatal("a run over a store it could not snapshot was allowed")
			}
			if !strings.Contains(err.Error(), "snapshot the store") {
				t.Errorf("error does not say what failed: %v", err)
			}
			if fake.Ran() {
				t.Error("the child was started before the store could be read")
			}
		})
	}
}

// A store that has no candidates directory holds no candidates. The run's
// grant cannot create one, so it is absent after the run as well.
func TestAStoreWithNoCandidatesDirectoryHoldsNoCandidates(t *testing.T) {
	p := newProject(t)
	if err := os.RemoveAll(p.store.CandidatesPath()); err != nil {
		t.Fatalf("remove candidates/: %v", err)
	}

	if _, err := p.curate(t, nil, curator.Report{}, curator.Options{}); err != nil {
		t.Fatalf("a run over a store with no candidates directory was refused: %v", err)
	}
}

// The store the run leaves behind can be unreadable too, and then there is
// nothing to check its report against.
func TestAStoreTheRunLeavesUnreadableFails(t *testing.T) {
	p := newProject(t)
	after := p.fork(t)
	replaceWithFile(t, after.store.NotesPath())

	_, err := p.curate(t, after, curator.Report{}, curator.Options{})
	if err == nil || !strings.Contains(err.Error(), "snapshot the store") {
		t.Fatalf("error = %v, want the snapshot failure named", err)
	}
}

// An index that cannot be read is not a current one.
func TestAnIndexTheRunLeavesUnreadableFails(t *testing.T) {
	p := newProject(t)
	after := p.fork(t)
	if err := os.Remove(after.store.IndexPath()); err != nil {
		t.Fatalf("remove INDEX.md: %v", err)
	}
	if err := os.Mkdir(after.store.IndexPath(), 0o755); err != nil {
		t.Fatalf("mkdir INDEX.md: %v", err)
	}

	_, err := p.curate(t, after, curator.Report{}, curator.Options{})
	wantRefused(t, err, "INDEX.md could not be checked")
}

// A report is held to what the run started from: a candidate it says it
// resolved, or a note it says it retracted, that the store never held is a
// report about some other run — its absence afterwards proves nothing.
func TestAReportNamingWhatWasNeverThereFails(t *testing.T) {
	for name, tc := range map[string]struct {
		report curator.Report
		want   string
	}{
		"a candidate never staged": {curator.Report{CandidatesResolved: []string{"20260901-never-staged"}}, "was never staged"},
		"a note never in notes/":   {curator.Report{NotesRetracted: []string{"never-a-note"}}, "was never in notes/"},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProject(t)

			_, err := p.curate(t, p.fork(t), tc.report, curator.Options{})
			wantRefused(t, err, tc.want)
		})
	}
}

// A note the report says was touched has to parse: one that does not has no
// anchors anybody can check, and reads as absent to every reader.
func TestATouchedNoteThatDoesNotParseFails(t *testing.T) {
	p := newProject(t)
	after := p.fork(t)
	writeFile(t, after.notePath("garbled"), "no frontmatter here\n")

	_, err := p.curate(t, after, curator.Report{NotesTouched: []string{"garbled"}}, curator.Options{})
	wantRefused(t, err, `note "garbled"`, "does not parse")
}

// A completion report that is present but not the shape the schema asks for
// still comes back with the curator's own account of the run.
func TestAReportThatDoesNotParseKeepsTheCuratorsAccount(t *testing.T) {
	envelope := `{"type":"result","subtype":"success","is_error":false,"session_id":"m1","result":"I did some things","structured_output":["not","an","object"]}`

	_, res, err := run(t, envelope, curator.Options{})
	if err == nil || !strings.Contains(err.Error(), "does not parse") {
		t.Fatalf("error = %v, want the malformed report named", err)
	}
	if !strings.Contains(res.Text, "I did some things") {
		t.Errorf("Text = %q, want the curator's account kept beside the error", res.Text)
	}
}
