package definitions

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// EntryPoint is the location of a single definition's entry-point file
// inside a catalog. Bundled resources (e.g. the prompts/ subdirectory of a
// skill) are intentionally excluded.
type EntryPoint struct {
	Category Category
	Path     string // absolute path to the entry-point file
	RelPath  string // path relative to definitions/<category>/
}

// WalkCatalog walks the definitions/ tree under root and returns one
// EntryPoint per definition. The function is shared between the catalog
// smoke test and any future code (resolver, agtk doctor) that needs to
// enumerate the catalog.
func WalkCatalog(root string) ([]EntryPoint, error) {
	defsDir := filepath.Join(root, "definitions")
	var out []EntryPoint
	err := filepath.WalkDir(defsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(defsDir, path)
		if err != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 2 {
			return nil
		}
		cat := CategoryFromDir(parts[0])
		if cat == "" {
			return nil
		}
		relWithinCat := strings.Join(parts[1:], "/")
		if !isEntryPoint(cat, relWithinCat) {
			return nil
		}
		out = append(out, EntryPoint{
			Category: cat,
			Path:     path,
			RelPath:  relWithinCat,
		})
		return nil
	})
	return out, err
}

// isEntryPoint returns true when relWithinCat names the canonical
// entry-point file for cat. Bundled resources return false.
func isEntryPoint(cat Category, relWithinCat string) bool {
	parts := strings.Split(relWithinCat, "/")
	switch cat {
	case CategorySkill:
		// <name>/SKILL.md exactly.
		return len(parts) == 2 && strings.EqualFold(parts[1], "SKILL.md")
	case CategoryAgent, CategoryRule, CategoryInstruction:
		return len(parts) == 1 && strings.EqualFold(filepath.Ext(parts[0]), ".md")
	case CategoryCommand:
		// any depth, must end in .md
		return strings.EqualFold(filepath.Ext(parts[len(parts)-1]), ".md")
	case CategoryHook, CategoryMCP:
		ext := strings.ToLower(filepath.Ext(parts[len(parts)-1]))
		return len(parts) == 1 && (ext == ".yaml" || ext == ".yml")
	}
	return false
}
