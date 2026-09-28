package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-driver/agentictest"

	"github.com/pedromvgomes/agentic-toolkit/internal/curator"
	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

// anchoredFile is the source file every fixture note is anchored to, relative
// to the project root.
const anchoredFile = "internal/lockfile/pins.go"

// project is a repository holding a memory store, as a curation run finds it:
// scaffolded, with a current index and a source file for notes to anchor to.
type project struct {
	root  string
	store *memory.Store
}

func newProject(t *testing.T) *project {
	t.Helper()
	return newProjectIn(t, t.TempDir())
}

func newProjectIn(t *testing.T, root string) *project {
	t.Helper()
	writeFile(t, filepath.Join(root, filepath.FromSlash(anchoredFile)), "package lockfile\n")
	store := memory.New(root, "")
	if err := store.Scaffold(); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	p := &project{root: root, store: store}
	p.reindex(t)
	return p
}

// fork copies p's store to a directory of its own, still rooted at p's project
// so its anchors resolve against the same files. It is the store a run leaves
// behind: a test edits the fork into the state the run should end in, and the
// child in curate turns p's store into it.
func (p *project) fork(t *testing.T) *project {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "store")
	if err := os.CopyFS(dir, os.DirFS(p.store.Root)); err != nil {
		t.Fatalf("fork the store: %v", err)
	}
	return &project{root: p.root, store: memory.New(p.root, dir)}
}

func (p *project) notePath(name string) string {
	return filepath.Join(p.store.NotesPath(), name+memory.NoteExt)
}

func (p *project) candidatePath(id string) string {
	return filepath.Join(p.store.CandidatesPath(), id+memory.NoteExt)
}

// writeNote writes a well-formed note anchored to anchoredFile. Stamped, it is
// what `agtk memory anchor` leaves; unstamped, its anchor carries no hash.
func (p *project) writeNote(t *testing.T, name string, stamped bool) {
	t.Helper()
	path := p.notePath(name)
	raw := "---\nname: " + name + "\nkind: invariant\n" +
		"description: Lock resolution pins commit SHAs, never tags.\n" +
		"anchors:\n  - path: " + anchoredFile + "\n" +
		"confidence: verified\n---\n\n" +
		"The lockfile records a commit SHA for every ref; see " + anchoredFile + ".\n"
	writeFile(t, path, raw)
	if !stamped {
		return
	}
	n, err := memory.ParseNote(path, []byte(raw))
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	if _, err := p.store.Stamp(n); err != nil {
		t.Fatalf("stamp %s: %v", name, err)
	}
}

// editNote changes a note's body and nothing else, so it stays stamped and
// lint-clean and differs from its earlier self only in its bytes.
func (p *project) editNote(t *testing.T, name string) {
	t.Helper()
	path := p.notePath(name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	writeFile(t, path, string(raw)+"\nThe same holds for nested extends: graphs.\n")
}

func (p *project) stage(t *testing.T, id string) {
	t.Helper()
	writeFile(t, p.candidatePath(id),
		"---\nabout: lock resolution pins SHAs\nsaw:\n  - "+anchoredFile+"\n---\n\nSee "+anchoredFile+".\n")
}

func (p *project) remove(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
}

// reindex regenerates INDEX.md, as `agtk memory index` would.
func (p *project) reindex(t *testing.T) {
	t.Helper()
	notes, errs := p.store.LoadNotes()
	if len(errs) > 0 {
		t.Fatalf("load notes: %v", errs)
	}
	if _, err := p.store.WriteIndex(notes); err != nil {
		t.Fatalf("index: %v", err)
	}
}

// curate runs the curator over p with a child that leaves p's store exactly as
// after holds it, then files report as its completion report.
//
// The fake answers with canned stdout and touches nothing, so what a real run
// would have done to the store is replayed by a wrapper around it instead —
// inside the child, which is the one place it lands after Run's snapshot and
// before Run's check. A nil after leaves the store as the run found it.
func (p *project) curate(t *testing.T, after *project, report curator.Report, opts curator.Options) (curator.Result, error) {
	t.Helper()
	fake := (&agentictest.Fake{Stdout: reportEnvelope(t, report)}).Build(t)

	var script strings.Builder
	script.WriteString("#!/bin/sh\n")
	if after != nil {
		script.WriteString("rm -rf " + shellQuote(p.store.Root) + " && cp -R " +
			shellQuote(after.store.Root) + " " + shellQuote(p.store.Root) + " || exit 97\n")
	}
	script.WriteString("exec " + shellQuote(fake.Path()) + " \"$@\"\n")
	child := filepath.Join(t.TempDir(), "curator-child")
	// #nosec G306 -- a script that must be executable; owner-only, in a test temp dir
	if err := os.WriteFile(child, []byte(script.String()), 0o700); err != nil {
		t.Fatalf("write the child: %v", err)
	}

	opts.Provider = "claudecode"
	opts.Binary = child
	opts.WorkDir = p.root
	opts.StoreRoot = p.store.Root
	opts.NotesDir = p.store.NotesPath()
	opts.CandidatesDir = p.store.CandidatesPath()
	res, err := curator.Run(t.Context(), opts)
	if !fake.Ran() {
		t.Fatalf("the child never reached the fake: %v", err)
	}
	return res, err
}

// reportEnvelope is a successful turn whose completion report is r.
func reportEnvelope(t *testing.T, r curator.Report) string {
	t.Helper()
	nonNil := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}
	structured, err := json.Marshal(map[string][]string{
		"candidatesResolved": nonNil(r.CandidatesResolved),
		"notesRetracted":     nonNil(r.NotesRetracted),
		"notesTouched":       nonNil(r.NotesTouched),
	})
	if err != nil {
		t.Fatalf("marshal the report: %v", err)
	}
	return `{"type":"result","subtype":"success","is_error":false,"session_id":"v1","result":"curated","structured_output":` +
		string(structured) + `}`
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
