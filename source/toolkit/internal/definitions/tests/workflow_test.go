package tests

import (
	"bytes"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
)

func TestWorkflowIsACategoryUnderTheWorkflowsDirectory(t *testing.T) {
	found := false
	for _, c := range definitions.AllCategories {
		if c == definitions.CategoryWorkflow {
			found = true
		}
	}
	if !found {
		t.Fatalf("CategoryWorkflow not in AllCategories: %v", definitions.AllCategories)
	}
	if got := definitions.CategoryWorkflow.CategoryDir(); got != "workflows" {
		t.Errorf("CategoryDir() = %q, want 'workflows'", got)
	}
	if got := definitions.CategoryFromDir("workflows"); got != definitions.CategoryWorkflow {
		t.Errorf("CategoryFromDir('workflows') = %q, want CategoryWorkflow", got)
	}
}

func TestAWorkflowIsNamedByItsIDAndDescribedByItsTitle(t *testing.T) {
	const path = "definitions/workflows/triage-issue.yaml"
	def, err := definitions.ParseInCatalog(validFS(), path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	w, ok := def.(*definitions.Workflow)
	if !ok {
		t.Fatalf("got %T, want *Workflow", def)
	}
	if def.Category() != definitions.CategoryWorkflow {
		t.Errorf("category = %q, want %q", def.Category(), definitions.CategoryWorkflow)
	}
	if w.ID != "triage-issue" || w.Name != "triage-issue" {
		t.Errorf("id = %q, name = %q, want both 'triage-issue'", w.ID, w.Name)
	}
	if w.Title != "Triage an issue" || w.Description != "Triage an issue" {
		t.Errorf("title = %q, description = %q, want both 'Triage an issue'", w.Title, w.Description)
	}
}

func TestAWorkflowKeepsItsSourceByteForByte(t *testing.T) {
	const path = "definitions/workflows/triage-issue.yaml"
	raw, err := fs.ReadFile(validFS(), path)
	if err != nil {
		t.Fatal(err)
	}
	def, err := definitions.ParseInCatalog(validFS(), path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := def.(*definitions.Workflow).Source; !bytes.Equal(got, raw) {
		t.Errorf("source differs from the file:\n got: %q\nwant: %q", got, raw)
	}
}

func TestAWorkflowAcceptsHatuaFieldsTheToolkitDoesNotModel(t *testing.T) {
	fsys := fstest.MapFS{
		"definitions/workflows/full.yaml": &fstest.MapFile{Data: []byte("" +
			"id: full\n" +
			"name: Full\n" +
			"version: 3\n" +
			"status: draft\n" +
			"connections:\n" +
			"  - id: gh\n" +
			"    ref: null\n" +
			"vars:\n" +
			"  - key: count\n" +
			"    t: number\n" +
			"    value: 0\n" +
			"blocks: []\n" +
			"steps: []\n",
		)},
	}
	if _, err := definitions.ParseInCatalog(fsys, "definitions/workflows/full.yaml"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAWorkflowWithoutATitleHasNoDescription(t *testing.T) {
	fsys := fstest.MapFS{
		"definitions/workflows/untitled.yaml": &fstest.MapFile{Data: []byte("id: untitled\nsteps: []\n")},
	}
	def, err := definitions.ParseInCatalog(fsys, "definitions/workflows/untitled.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := def.GetCommon().Description; got != "" {
		t.Errorf("description = %q, want empty", got)
	}
}

func TestInvalidWorkflowsAreRejected(t *testing.T) {
	cases := []struct {
		name string
		path string
		want definitions.ErrorKind
	}{
		{"id-differs-from-filename", "definitions/workflows/id-mismatch.yaml", definitions.ErrInvalidName},
		{"id-missing", "definitions/workflows/missing-id.yaml", definitions.ErrMissingRequired},
		{"nested-path", "definitions/workflows/nested/inner.yaml", definitions.ErrInvalidName},
		{"malformed-yaml", "definitions/workflows/bad-yaml.yaml", definitions.ErrYAMLSyntax},
	}
	fsys := invalidFS()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := definitions.ParseInCatalog(fsys, tc.path)
			if err == nil {
				t.Fatalf("expected error of kind %s, got nil", tc.want)
			}
			if !definitions.IsKind(err, tc.want) {
				t.Errorf("got error kind != %s\n  err: %v", tc.want, err)
			}
		})
	}
}

func TestAnEmptyWorkflowFileIsMissingItsID(t *testing.T) {
	fsys := fstest.MapFS{
		"definitions/workflows/empty.yaml": &fstest.MapFile{Data: []byte("\n")},
	}
	_, err := definitions.ParseInCatalog(fsys, "definitions/workflows/empty.yaml")
	if !definitions.IsKind(err, definitions.ErrMissingRequired) {
		t.Errorf("got %v, want ErrMissingRequired", err)
	}
}

func TestParseFile_WorkflowIsNamedByItsIDNotItsFilename(t *testing.T) {
	fsys := fstest.MapFS{
		"anything.yaml": &fstest.MapFile{Data: []byte("id: release\nname: Release\nsteps: []\n")},
	}
	def, err := definitions.ParseFile(fsys, definitions.CategoryWorkflow, "anything.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := def.GetCommon().Name; got != "release" {
		t.Errorf("name = %q, want the id 'release'", got)
	}
}

func TestParseFile_WorkflowWithoutAnIDIsRejected(t *testing.T) {
	fsys := fstest.MapFS{
		"release.yaml": &fstest.MapFile{Data: []byte("name: Release\nsteps: []\n")},
	}
	_, err := definitions.ParseFile(fsys, definitions.CategoryWorkflow, "release.yaml")
	if !definitions.IsKind(err, definitions.ErrMissingRequired) {
		t.Errorf("got %v, want ErrMissingRequired", err)
	}
}

func TestParseFile_WorkflowRejectsANestedID(t *testing.T) {
	fsys := fstest.MapFS{
		"x.yaml": &fstest.MapFile{Data: []byte("id: a/b\nsteps: []\n")},
	}
	_, err := definitions.ParseFile(fsys, definitions.CategoryWorkflow, "x.yaml")
	if !definitions.IsKind(err, definitions.ErrInvalidName) {
		t.Errorf("got %v, want ErrInvalidName", err)
	}
}

func TestParseBundle_RejectsWorkflows(t *testing.T) {
	fsys := fstest.MapFS{
		"x.yaml": &fstest.MapFile{Data: []byte("id: x\nsteps: []\n")},
	}
	_, err := definitions.ParseBundle(fsys, definitions.CategoryWorkflow, "x")
	if !definitions.IsKind(err, definitions.ErrUnknownCategory) {
		t.Errorf("got %v, want ErrUnknownCategory", err)
	}
}

func TestWalkCatalogFindsOnlyFlatYAMLFilesUnderWorkflows(t *testing.T) {
	fsys := fstest.MapFS{
		"definitions/workflows/a.yaml":        &fstest.MapFile{Data: []byte("id: a\n")},
		"definitions/workflows/b.yml":         &fstest.MapFile{Data: []byte("id: b\n")},
		"definitions/workflows/README.md":     &fstest.MapFile{Data: []byte("# notes\n")},
		"definitions/workflows/nested/c.yaml": &fstest.MapFile{Data: []byte("id: c\n")},
	}
	entries, err := definitions.WalkCatalog(fsys)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	got := map[string]definitions.Category{}
	for _, e := range entries {
		got[e.RelPath] = e.Category
	}
	want := map[string]definitions.Category{
		"a.yaml": definitions.CategoryWorkflow,
		"b.yml":  definitions.CategoryWorkflow,
	}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for rel, cat := range want {
		if got[rel] != cat {
			t.Errorf("entry %q = %q, want %q", rel, got[rel], cat)
		}
	}
}
