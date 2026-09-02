package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

// project builds a throwaway project tree with a store in it and returns
// the store. files maps project-relative paths to contents.
func project(t *testing.T, files map[string]string) *memory.Store {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		write(t, filepath.Join(root, filepath.FromSlash(rel)), content)
	}
	store := memory.New(root, "")
	if err := store.Scaffold(); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	return store
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeNote drops a note into the store and returns its path.
func writeNote(t *testing.T, s *memory.Store, name, content string) string {
	t.Helper()
	path := filepath.Join(s.NotesPath(), name+memory.NoteExt)
	write(t, path, content)
	return path
}

// note is the canonical well-formed note, parameterised by name and the
// anchors block so tests can vary one thing at a time.
func note(name, anchors string) string {
	return "---\nname: " + name + "\nkind: invariant\n" +
		"description: Lock resolution pins commit SHAs, never tags.\n" +
		"anchors:\n" + anchors +
		"confidence: verified\n---\n\n" +
		"`agtk lock` resolves extends: graphs to SHAs — see internal/resolver/graph.go:88.\n"
}

func loadOne(t *testing.T, s *memory.Store, name string) *memory.Note {
	t.Helper()
	notes, errs := s.LoadNotes()
	if len(errs) > 0 {
		t.Fatalf("load notes: %v", errs)
	}
	for _, n := range notes {
		if n.Name == name {
			return n
		}
	}
	t.Fatalf("note %q not found", name)
	return nil
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}
