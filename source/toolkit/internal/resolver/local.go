package resolver

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
)

// scanLocal resolves every definition under the entry manifest's `local:`
// directories and merges it into the overlay, returning one stack identifier
// per category that produced at least one definition.
//
// The caller appends those identifiers to Plan.StackOrder AFTER the entry
// manifest's own identifier. Adapters that merge order-dependent values (the
// settings and config key merges) index StackName into StackOrder, and an
// identifier that is not in that list reads as index -1 — sorting FIRST, so a
// locally scanned definition would lose every merge against a stack reached
// through extends:. Local definitions are the consumer's own and win last.
//
// A category with no definitions contributes no identifier: StackOrder names
// the stacks that actually contributed.
func (s *traversalState) scanLocal(lc *stack.LocalConfig, root string, ctx stackCtx) []string {
	var ids []string
	for _, ld := range localDirs(lc) {
		dir := joinFromFile(ctx.FilePathInFS, ld.dir)
		found, err := s.scanLocalCategory(ld.cat, dir, root, ctx)
		if err != nil {
			// Every configured category is scanned even after one fails, so a
			// manifest with several wrong paths reports all of them at once.
			s.errs = append(s.errs, fmt.Errorf("local.%s (%q): %w", ld.cat.CategoryDir(), ld.dir, err))
			continue
		}
		if len(found) == 0 {
			continue
		}
		ids = append(ids, localStackName(ld.cat))
		for _, w := range found {
			key := defKey{Category: w.Category, Name: w.Name}
			if prev, exists := s.overlay[key]; exists {
				s.diags = append(s.diags, Diagnostic{
					Kind: DiagOverride,
					Message: fmt.Sprintf("%s/%s from %s was overridden by entry from stack %q",
						w.Category.CategoryDir(), w.Name, prev.SourceURL, displayID(w.StackName)),
					Category:  w.Category,
					Name:      w.Name,
					SourceURL: prev.SourceURL,
					StackName: w.StackName,
				})
			}
			s.overlay[key] = w
		}
	}
	return ids
}

// localStackName is the identifier a locally scanned definition carries as its
// StackName and contributes to Plan.StackOrder.
func localStackName(cat definitions.Category) string {
	return "local." + cat.CategoryDir()
}

// localDir pairs a configured directory with the category it is scanned for.
type localDir struct {
	cat definitions.Category
	dir string
}

// localDirs lists the configured scan directories in category order, skipping
// the ones the manifest leaves empty.
//
// local.context names a single file rather than a directory and is resolved
// separately.
func localDirs(lc *stack.LocalConfig) []localDir {
	all := []localDir{
		{definitions.CategorySkill, lc.Skills},
		{definitions.CategoryAgent, lc.Agents},
		{definitions.CategoryRule, lc.Rules},
		{definitions.CategoryInstruction, lc.Instructions},
		{definitions.CategoryCommand, lc.Commands},
		{definitions.CategoryHook, lc.Hooks},
		{definitions.CategoryMCP, lc.MCP},
		{definitions.CategorySetting, lc.Settings},
	}
	out := make([]localDir, 0, len(all))
	for _, ld := range all {
		if ld.dir != "" {
			out = append(out, ld)
		}
	}
	return out
}

// scanLocalCategory parses every definition the category's layout finds in dir.
//
// Two files declaring the same `name:` are refused rather than one silently
// winning: which of them the consumer meant is not knowable here, and a scan
// that drops a definition without saying so renders an incomplete repo.
func (s *traversalState) scanLocalCategory(cat definitions.Category, dir, root string, ctx stackCtx) ([]walkedDef, error) {
	files, err := localFiles(cat, ctx.FS, dir)
	if err != nil {
		return nil, err
	}

	var (
		out  []walkedDef
		errs []error
		seen = map[string]string{} // definition name → the file that claimed it
	)
	for i, f := range files {
		w, err := s.parseFromFS(ctx.FS, f.bundleDir, f.fileName, cat, f.expectedName, ctx, root)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path.Join(f.bundleDir, f.fileName), err))
			continue
		}
		// Local definitions are the consumer's own; the identifier records that
		// rather than the entry manifest's, so they sort last in adapter merges.
		w.StackName = localStackName(cat)
		if cat == definitions.CategoryInstruction {
			// Filename order is the consumer's ordering knob for the one
			// category whose definitions are concatenated into a single file.
			w.ScanOrder = i + 1
		}
		if prev, dup := seen[w.Name]; dup {
			errs = append(errs, fmt.Errorf("%q and %q both declare the name %q",
				prev, path.Join(f.bundleDir, f.fileName), w.Name))
			continue
		}
		seen[w.Name] = path.Join(f.bundleDir, f.fileName)
		out = append(out, *w)
	}
	if len(errs) > 0 {
		// Every file in the directory is parsed even after one fails, for the
		// same reason every category is scanned even after one fails.
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// localFile is one definition the scan found: the arguments parseFromFS needs
// to read it.
type localFile struct {
	bundleDir    string
	fileName     string
	expectedName string
}

// localFiles lists the definition files in dir for cat, inverting the bare-name
// layout: where a bare entry names a file under <root>/<plural>/<name>, a scan
// takes every file the same shape allows.
//
//   - skill, agent: one subdirectory per definition, holding SKILL.md / AGENT.md
//   - rule, instruction: *.md directly in dir
//   - command: *.md at any depth; the name is the relative path minus extension,
//     so nested files are namespaced ("foo/bar")
//   - hook, mcp, setting: *.yaml / *.yml directly in dir
//
// Entries the shape does not cover are ignored: a README beside the definitions
// is not a failed definition.
func localFiles(cat definitions.Category, fsys fs.FS, dir string) ([]localFile, error) {
	if cat == definitions.CategoryCommand {
		return localCommandFiles(fsys, dir)
	}

	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var out []localFile
	for _, e := range entries {
		if isBundleCategory(cat) {
			if !e.IsDir() {
				continue
			}
			entryFile := "SKILL.md"
			if cat == definitions.CategoryAgent {
				entryFile = "AGENT.md"
			}
			out = append(out, localFile{
				bundleDir:    path.Join(dir, e.Name()),
				fileName:     entryFile,
				expectedName: e.Name(),
			})
			continue
		}
		if e.IsDir() || !hasExt(e.Name(), localExts(cat)) {
			continue
		}
		out = append(out, localFile{bundleDir: dir, fileName: e.Name()})
	}
	return out, nil
}

// localCommandFiles walks dir recursively; the walk's own lexicographic order
// carries through to the scan order.
func localCommandFiles(fsys fs.FS, dir string) ([]localFile, error) {
	if _, err := fs.ReadDir(fsys, dir); err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}
	var out []localFile
	err := fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Ext(p) != ".md" {
			return nil
		}
		rel, relErr := relativeTo(dir, p)
		if relErr != nil {
			return relErr
		}
		out = append(out, localFile{bundleDir: dir, fileName: rel})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk directory: %w", err)
	}
	return out, nil
}

// relativeTo strips the scanned directory from a walked path, leaving the
// fs-relative name the command's namespacing is derived from.
func relativeTo(dir, p string) (string, error) {
	if dir == "" || dir == "." {
		return p, nil
	}
	rel := strings.TrimPrefix(p, dir+"/")
	if rel == p {
		return "", fmt.Errorf("%q is not under %q", p, dir)
	}
	return rel, nil
}

// localExts is the set of extensions a file-shaped category's scan accepts.
func localExts(cat definitions.Category) []string {
	switch cat {
	case definitions.CategoryHook, definitions.CategoryMCP, definitions.CategorySetting:
		return []string{".yaml", ".yml"}
	default:
		return []string{".md"}
	}
}

func hasExt(name string, exts []string) bool {
	ext := path.Ext(name)
	for _, want := range exts {
		if ext == want {
			return true
		}
	}
	return false
}
