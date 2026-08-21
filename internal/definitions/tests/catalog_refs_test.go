package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
)

// catalogNames indexes the real definitions/ tree as category -> set of
// definition names, so string references elsewhere in the repo can be
// checked against what the catalog actually contains.
func catalogNames(t *testing.T, root string) map[definitions.Category]map[string]bool {
	t.Helper()
	fsys := os.DirFS(root)
	entries, err := definitions.WalkCatalog(fsys)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	index := map[definitions.Category]map[string]bool{}
	for _, e := range entries {
		def, err := definitions.ParseInCatalog(fsys, e.Path)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Path, err)
		}
		if index[e.Category] == nil {
			index[e.Category] = map[string]bool{}
		}
		index[e.Category][def.GetCommon().Name] = true
	}
	return index
}

// TestRequiresResolve asserts every `requires:` entry in the real catalog
// points at a definition the catalog actually holds. Nothing in the render
// path reads Requires, so a typo in a cross-reference is otherwise inert
// until a human notices the dependency was never pulled in.
func TestRequiresResolve(t *testing.T) {
	root := repoRoot(t)
	fsys := os.DirFS(root)
	index := catalogNames(t, root)

	entries, err := definitions.WalkCatalog(fsys)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	for _, e := range entries {
		def, err := definitions.ParseInCatalog(fsys, e.Path)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Path, err)
		}
		for _, req := range def.GetCommon().Requires {
			t.Run(e.Path+"->"+req, func(t *testing.T) {
				// Canonical form is "<category-dir>/<name>", e.g.
				// "skills/challenge" — the directory name, which is the
				// category plus an "s".
				dir, name, ok := strings.Cut(req, "/")
				if !ok {
					t.Fatalf("requires %q is not in 'category/name' form", req)
				}
				cat := definitions.Category(strings.TrimSuffix(dir, "s"))
				names, known := index[cat]
				if !known {
					t.Fatalf("requires %q names unknown category %q", req, dir)
				}
				if !names[name] {
					t.Errorf("requires %q does not resolve: no %s named %q in definitions/", req, cat, name)
				}
			})
		}
	}
}

// TestStackManifestsResolve asserts every bare-name entry in the repo's
// published stacks/*.yaml exists in the real definitions/ tree. These
// manifests are the contract consumers extend by URL, so a name that has
// been renamed or deleted breaks their sync, not ours — and no other test
// loads the real manifests against the real catalog.
func TestStackManifestsResolve(t *testing.T) {
	root := repoRoot(t)
	index := catalogNames(t, root)

	manifests, err := filepath.Glob(filepath.Join(root, "stacks", "*.yaml"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(manifests) == 0 {
		t.Fatalf("no stack manifests found under %s/stacks", root)
	}

	for _, path := range manifests {
		t.Run(filepath.Base(path), func(t *testing.T) {
			st, err := stack.ParseFile(path)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			for _, cat := range definitions.AllCategories {
				for _, ref := range st.EntriesFor(cat) {
					// Only bare names are resolvable against this repo's
					// catalog; URL and path refs point elsewhere.
					if ref.Kind != stack.RefBare {
						continue
					}
					if !index[cat][ref.Name] {
						t.Errorf("%s entry %q does not resolve: no %s named %q in definitions/",
							cat, ref.Raw, cat, ref.Name)
					}
				}
			}
		})
	}
}
