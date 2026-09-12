package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The schema docs are generated from the struct definitions and committed. A
// struct change that nobody regenerates for leaves the documented schema
// describing a shape the code no longer parses, and the "DO NOT EDIT" banner
// means no reader has any reason to distrust it.
func TestCommittedSchemaDocsMatchTheStructs(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}

	for _, doc := range []struct {
		path string
		gen  func() ([]byte, error)
	}{
		{filepath.Join(catalogDir, "SCHEMA.md"), render},
		{filepath.Join(catalogDir, "CONFIG-SCHEMA.md"), renderConfig},
	} {
		t.Run(doc.path, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join(root, doc.path))
			if err != nil {
				t.Fatalf("read committed doc: %v", err)
			}
			got, err := doc.gen()
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("%s is out of step with the structs it documents; run `go generate ./...`", doc.path)
			}
		})
	}
}
