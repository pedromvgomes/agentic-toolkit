package schemadoc

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// Run from outside a repository, the generator has no idea where the catalog
// is. Walking to the filesystem root and writing the docs relative to wherever
// it stopped would scatter them somewhere nobody reads; refusing says so.
func TestRepoRootRefusesOutsideARepository(t *testing.T) {
	t.Chdir(t.TempDir())

	root, err := repoRoot()
	if err == nil {
		t.Fatalf("repoRoot returned %q with no .git above the working directory", root)
	}
	if !strings.Contains(err.Error(), ".git") {
		t.Errorf("the error does not say what was looked for: %v", err)
	}
}

// The documents land in the catalog of the repo the generator runs from, and
// the directory is created if it is not there. Where they land is the whole
// job: a generator that writes them anywhere else leaves the committed docs
// stale while reporting success.
func TestGenerateWritesBothDocumentsIntoTheCatalog(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	t.Chdir(repo)

	if err := Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	for _, rel := range []string{
		filepath.Join(catalogDir, "SCHEMA.md"),
		filepath.Join(catalogDir, "CONFIG-SCHEMA.md"),
	} {
		got, err := os.ReadFile(filepath.Join(repo, rel))
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		if len(got) == 0 {
			t.Errorf("%s is empty", rel)
		}
	}
}

// Outside a repository there is no catalog to write into, so Generate refuses
// rather than writing the documents relative to wherever the walk stopped.
func TestGenerateRefusesOutsideARepository(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := Generate(); err == nil {
		t.Error("Generate wrote the documents with no .git above the working directory")
	}
}

type EmbeddedWithoutKey struct {
	Value string `yaml:"value"`
}

type EmbeddedUnderKey struct {
	Value string `yaml:"value"`
}

type NamedNested struct {
	Value string `yaml:"value"`
}

type subStructHolder struct {
	EmbeddedWithoutKey
	EmbeddedUnderKey `yaml:"keyed"`
	Nested           NamedNested `yaml:"nested"`
}

// An embedded struct with no yaml key is inlined into its parent's mapping,
// so its fields are the parent's and it gets no section of its own. One
// embedded under a key is a nested mapping like any named field, and does.
func TestOnlyStructsUnderAYAMLKeyAreDocumentedAsSubStructs(t *testing.T) {
	var titles []string
	for _, d := range collectSubStructs(reflect.TypeOf(subStructHolder{})) {
		titles = append(titles, d.Title)
	}
	want := "`keyed` (EmbeddedUnderKey),`nested` (NamedNested)"
	if got := strings.Join(titles, ","); got != want {
		t.Errorf("sub-structs = %s, want %s", got, want)
	}
}

type ownFieldsHolder struct {
	EmbeddedWithoutKey
	Value    string            `yaml:"value"`
	Skipped  string            `yaml:"-"`
	Inlined  map[string]string `yaml:",inline"`
	Optional string            `yaml:"optional,omitempty" agtkdoc:"An optional field."`
	Untagged string
}

// ownFields lists a struct's own document fields only: nothing embedded, and
// nothing the YAML document does not carry under a key of its own — a `-`
// field is absent from it, and an inline one is its parent's.
func TestOwnFieldsListsOnlyTheStructsOwnKeyedFields(t *testing.T) {
	var got []string
	for _, f := range ownFields(reflect.TypeOf(ownFieldsHolder{})) {
		got = append(got, fmt.Sprintf("%s:%t:%s", f.Name, f.Required, f.Description))
	}
	want := "value:true:,optional:false:An optional field.,Untagged:true:"
	if g := strings.Join(got, ","); g != want {
		t.Errorf("own fields = %s, want %s", g, want)
	}
}

func TestAnInlineFieldIsNotDocumentedAsAFieldOfItsOwn(t *testing.T) {
	f, _ := reflect.TypeOf(ownFieldsHolder{}).FieldByName("Inlined")
	if fd, ok := fieldDocFor(f); ok {
		t.Errorf("fieldDocFor(inline) = %+v, want no field", fd)
	}
}
