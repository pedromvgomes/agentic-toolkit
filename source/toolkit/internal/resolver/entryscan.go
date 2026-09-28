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

// contextName is the name the file `context:` points at becomes an
// instruction under, whatever the file is called. A fixed name is what lets
// the consumer rename or move the file without the rendered output changing.
const contextName = "context"

// loadEntry walks the entry manifest: the stacks it composes first, then
// what its own convention root holds.
//
// The manifest's identifier is "" and it is appended to the visit order
// last, so everything scanned under its root wins the order-dependent
// merges adapters run — the consumer's own content is the final word on its
// own repo.
func (s *traversalState) loadEntry(m *stack.EntryManifest, ctx stackCtx) {
	s.inDFS[ctx.Identifier] = true
	defer func() {
		s.inDFS[ctx.Identifier] = false
		s.visited[ctx.Identifier] = true
	}()

	for i, ref := range m.Stacks {
		if err := s.loadExtends(ref, ctx); err != nil {
			s.errs = append(s.errs, fmt.Errorf("entry manifest stacks[%d] (%q): %w", i, ref.Raw, err))
		}
	}

	s.scanEntryRoot(m, ctx)

	s.order = append(s.order, ctx.Identifier)
}

// scanEntryRoot resolves every definition the manifest's convention root
// holds, plus the instruction its `context:` names, and merges each one in.
//
// Everything found here carries the entry manifest's own identifier: a
// locally scanned definition is the manifest's contribution, reached by
// convention instead of by being listed.
func (s *traversalState) scanEntryRoot(m *stack.EntryManifest, ctx stackCtx) {
	root := joinFromFile(ctx.FilePathInFS, m.EffectiveRoot())

	for _, cat := range definitions.AllCategories {
		dir := path.Join(root, cat.CategoryDir())
		found, err := s.scanCategory(cat, dir, root, ctx)
		if err != nil {
			// Every category is scanned even after one fails, so a root with
			// several broken definitions reports all of them at once.
			s.errs = append(s.errs, fmt.Errorf("scanning %s: %w", dir, err))
			continue
		}
		for _, w := range found {
			s.merge(w)
		}
	}

	if m.Context == "" {
		return
	}
	w, err := s.readContext(m.Context, root, ctx)
	if err != nil {
		// A missing category directory is an empty one, but a named
		// `context:` file that is not there is a manifest describing content
		// this repo does not have.
		s.errs = append(s.errs, fmt.Errorf("context (%q): %w", m.Context, err))
		return
	}
	s.merge(*w)
}

// scanCategory parses every definition cat's layout finds in dir.
//
// A directory that does not exist is an empty one: a repo contributes the
// categories it has, and having to create seven empty directories to use the
// eighth is a worse manifest than no manifest.
//
// Two files declaring the same `name:` are refused rather than one silently
// winning: which of them the consumer meant is not knowable here, and a scan
// that drops a definition without saying so renders an incomplete repo.
func (s *traversalState) scanCategory(cat definitions.Category, dir, root string, ctx stackCtx) ([]walkedDef, error) {
	files, err := scanFiles(cat, ctx.FS, dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, err
	}

	var (
		out  []walkedDef
		errs []error
		seen = map[string]string{} // definition name → the file that claimed it
	)
	for _, f := range files {
		w, err := s.parseFromFS(ctx.FS, f.bundleDir, f.fileName, cat, f.expectedName, ctx, root)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path.Join(f.bundleDir, f.fileName), err))
			continue
		}
		w.Scanned = true
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

// readContext turns the file `context:` names into an instruction.
//
// The file is read verbatim: it is the consumer's own top-level prose, so it
// carries no frontmatter and is not put through the definition parser, which
// refuses a markdown definition that has none.
func (s *traversalState) readContext(file, root string, ctx stackCtx) (*walkedDef, error) {
	p := joinFromFile(ctx.FilePathInFS, file)
	raw, err := fs.ReadFile(ctx.FS, p)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	dirFS := ctx.FS
	if dir := path.Dir(p); dir != "" && dir != "." {
		sub, subErr := fs.Sub(ctx.FS, dir)
		if subErr != nil {
			return nil, fmt.Errorf("fs.Sub %q: %w", dir, subErr)
		}
		dirFS = sub
	}

	def := &definitions.Instruction{
		Common: definitions.Common{Name: contextName},
		Body:   string(raw),
	}
	return &walkedDef{
		Category:   definitions.CategoryInstruction,
		Name:       contextName,
		Definition: def,
		SourceURL:  ctx.SourceURL,
		SourceRef:  ctx.SourceRef,
		StackName:  ctx.Identifier,
		IsContext:  true,
		Scanned:    true,
		EntryPath:  path.Base(p),
		SourceFS:   dirFS,
		root:       root,
		ctx:        ctx,
	}, nil
}

// scannedFile is one definition the scan found: the arguments parseFromFS
// needs to read it.
type scannedFile struct {
	bundleDir    string
	fileName     string
	expectedName string
}

// scanFiles lists the definition files in dir for cat, inverting the
// bare-name layout: where a bare entry names a file under
// <root>/<plural>/<name>, a scan takes every file the same shape allows.
//
//   - skill, agent: one subdirectory per definition, holding SKILL.md / AGENT.md
//   - rule, instruction: *.md directly in dir
//   - command: *.md at any depth; the name is the relative path minus extension,
//     so nested files are namespaced ("foo/bar")
//   - hook, mcp, setting: *.yaml / *.yml directly in dir
//
// Entries the shape does not cover are ignored: a README beside the
// definitions is not a failed definition.
func scanFiles(cat definitions.Category, fsys fs.FS, dir string) ([]scannedFile, error) {
	if cat == definitions.CategoryCommand {
		return scanCommandFiles(fsys, dir)
	}

	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var out []scannedFile
	for _, e := range entries {
		if isBundleCategory(cat) {
			if !e.IsDir() {
				continue
			}
			entryFile := "SKILL.md"
			if cat == definitions.CategoryAgent {
				entryFile = "AGENT.md"
			}
			out = append(out, scannedFile{
				bundleDir:    path.Join(dir, e.Name()),
				fileName:     entryFile,
				expectedName: e.Name(),
			})
			continue
		}
		if e.IsDir() || !hasExt(e.Name(), scanExts(cat)) {
			continue
		}
		out = append(out, scannedFile{bundleDir: dir, fileName: e.Name()})
	}
	return out, nil
}

// scanCommandFiles walks dir recursively; the walk's own lexicographic order
// carries through to the scan order.
func scanCommandFiles(fsys fs.FS, dir string) ([]scannedFile, error) {
	if _, err := fs.ReadDir(fsys, dir); err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}
	var out []scannedFile
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
		out = append(out, scannedFile{bundleDir: dir, fileName: rel})
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

// scanExts is the set of extensions a file-shaped category's scan accepts.
func scanExts(cat definitions.Category) []string {
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
