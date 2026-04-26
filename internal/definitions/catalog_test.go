package definitions

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCatalogParses walks the real definitions/ tree at the repo root and
// asserts every entry-point file conforms to its category schema. Bundled
// resources (e.g. a skill's prompts/) are intentionally skipped via
// WalkCatalog, which only yields entry-point files.
func TestCatalogParses(t *testing.T) {
	root := repoRoot(t)
	defsDir := filepath.Join(root, "definitions")
	if _, err := os.Stat(defsDir); err != nil {
		t.Skipf("no definitions/ at %s: %v", defsDir, err)
	}

	entries, err := WalkCatalog(root)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("no entry-point files discovered under %s", defsDir)
	}

	for _, e := range entries {
		rel, _ := filepath.Rel(root, e.Path)
		t.Run(rel, func(t *testing.T) {
			def, err := ParseInCatalog(root, e.Path)
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}
			if def.GetCommon().Name == "" {
				t.Errorf("derived name is empty")
			}
		})
	}
}

// repoRoot finds the repo root by walking up from the test file's package
// directory looking for go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found walking up from %s", cwd)
		}
		dir = parent
	}
}
