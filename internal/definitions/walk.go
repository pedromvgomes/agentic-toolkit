package definitions

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
)

// DefinitionsDir is the canonical (forward-slash) location of the catalog
// inside a source filesystem.
const DefinitionsDir = "definitions"

// EntryPoint is the location of a single definition's entry-point file
// inside a source filesystem. Bundled resources (e.g. the prompts/
// subdirectory of a skill) are intentionally excluded.
type EntryPoint struct {
	Category Category
	// Path is forward-slash, relative to the source root, e.g.
	// "definitions/skills/foo/SKILL.md". Pass it back to ParseInCatalog
	// against the same fs.FS to read the file.
	Path string
	// RelPath is the same path with "definitions/<category>/" stripped.
	RelPath string
}

// WalkCatalog walks definitions/ inside fsys and returns one EntryPoint
// per definition. A missing definitions/ directory yields no entries and
// no error. Non-entry-point files (bundled resources, presets, README
// markdown, etc.) are skipped.
func WalkCatalog(fsys fs.FS) ([]EntryPoint, error) {
	info, err := fs.Stat(fsys, DefinitionsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	var out []EntryPoint
	err = fs.WalkDir(fsys, DefinitionsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(p, DefinitionsDir+"/")
		parts := strings.Split(rel, "/")
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
			Path:     p,
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
	case CategoryAgent:
		// <name>/AGENT.md exactly. Folder-shaped so the bundle can ship
		// companion files (prompts, tools, fixtures) alongside AGENT.md.
		return len(parts) == 2 && strings.EqualFold(parts[1], "AGENT.md")
	case CategoryRule, CategoryInstruction:
		return len(parts) == 1 && strings.EqualFold(filepath.Ext(parts[0]), ".md")
	case CategoryCommand:
		// any depth, must end in .md
		return strings.EqualFold(filepath.Ext(parts[len(parts)-1]), ".md")
	case CategoryHook, CategoryMCP, CategorySetting:
		ext := strings.ToLower(filepath.Ext(parts[len(parts)-1]))
		return len(parts) == 1 && (ext == ".yaml" || ext == ".yml")
	}
	return false
}
