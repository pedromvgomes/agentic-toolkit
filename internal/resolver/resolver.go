package resolver

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/sourceref"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
)

// Resolve walks an entry-point stack against the SourceProvider and
// returns a Plan.
//
// entry is the parsed entry-point stack (typically the consumer's
// .agentic-toolkit.yaml). entryFS is the filesystem that holds the
// entry-point file: bare-name and ./path lookups in the entry-point
// stack resolve against entryFS, with the entry-point file at
// entryPathInFS within it. Tests can pass arbitrary fs.FS values; the
// CLI passes os.DirFS(filepath.Dir(configPath)) and the basename as
// entryPathInFS.
//
// On any failure during DAG traversal or definition resolution, errors
// are joined and no Plan is returned. A failure to provide a source is
// surfaced as an error against the entry that triggered the fetch; the
// resolver continues with the rest of the work to give a complete
// failure picture.
func Resolve(entry *stack.Stack, entryFS fs.FS, entryPathInFS string, provider SourceProvider) (*Plan, error) {
	if entry == nil {
		return nil, errors.New("resolver: nil entry stack")
	}
	if entryFS == nil {
		return nil, errors.New("resolver: nil entryFS")
	}
	if provider == nil {
		return nil, errors.New("resolver: nil SourceProvider")
	}

	st := newTraversalState(provider)

	// Entry-point stack uses the empty string as its identifier and "" for
	// its source URL/Ref (it lives in the consumer's local FS, not in any
	// fetched source).
	entryCtx := stackCtx{
		Identifier:    "",
		SourceURL:     "",
		SourceRef:     "",
		FS:            entryFS,
		FilePathInFS:  entryPathInFS,
	}
	if err := st.loadStack(entry, entryCtx); err != nil {
		st.errs = append(st.errs, err)
	}

	if len(st.errs) > 0 {
		return nil, errors.Join(st.errs...)
	}

	// Collect winners → ordered Definitions.
	defs := make([]PlannedDefinition, 0, len(st.overlay))
	for _, w := range st.overlay {
		defs = append(defs, PlannedDefinition{
			Category:   w.Category,
			Name:       w.Name,
			Definition: w.Definition,
			SourceURL:  w.SourceURL,
			SourceRef:  w.SourceRef,
			StackName:  w.StackName,
			EntryPath:  w.EntryPath,
			SourceFS:   w.SourceFS,
		})
	}
	sort.Slice(defs, func(i, j int) bool {
		if defs[i].Category != defs[j].Category {
			return defs[i].Category < defs[j].Category
		}
		return defs[i].Name < defs[j].Name
	})

	// Order sources: SourceStack first (in visit order), then
	// SourceDefinition sorted by (URL, Ref).
	plannedSources := st.orderedSources()

	return &Plan{
		Stack:       entry,
		StackOrder:  st.order,
		Sources:     plannedSources,
		Definitions: defs,
		Diagnostics: st.diags,
	}, nil
}

// ===== traversal state =====

type traversalState struct {
	provider SourceProvider
	visited  map[string]bool       // stack identifiers fully processed
	inDFS    map[string]bool       // stack identifiers on the current DFS path
	order    []string              // identifiers in post-order visit
	overlay  map[defKey]walkedDef  // current (category, name) → winner
	sources  *sourceTable
	diags    []Diagnostic
	errs     []error
}

func newTraversalState(provider SourceProvider) *traversalState {
	return &traversalState{
		provider: provider,
		visited:  map[string]bool{},
		inDFS:    map[string]bool{},
		overlay:  map[defKey]walkedDef{},
		sources:  newSourceTable(),
	}
}

// stackCtx is the context for a single stack file: where it lives in
// which FS, what its provenance is for definitions loaded from it.
type stackCtx struct {
	// Identifier is the unique key for this stack in the visit graph. For
	// external stacks: "<url>@<ref>". For local-path stacks reached via a
	// path-extends: "<parentSourceURL>@<parentSourceRef>:<filePath>". For
	// the entry-point: "".
	Identifier string

	// SourceURL/SourceRef record this stack file's source for downstream
	// PlannedDefinition.SourceURL/SourceRef on entries loaded from it.
	// Empty for the entry-point.
	SourceURL string
	SourceRef string

	// FS is the filesystem this stack file lives in. Used for resolving
	// the stack's bare-name and ./path entries and ./path extends.
	FS fs.FS

	// FilePathInFS is the path of the stack file within FS. Used as the
	// base directory for ./path resolution.
	FilePathInFS string
}

// walkedDef is the intermediate per-entry record during traversal.
type walkedDef struct {
	Category   definitions.Category
	Name       string
	Definition definitions.Definition
	SourceURL  string
	SourceRef  string
	StackName  string
	EntryPath  string
	SourceFS   fs.FS
}

type defKey struct {
	Category definitions.Category
	Name     string
}

// loadStack runs depth-first post-order: extends children first, then
// this stack's own entries, then record the visit. Cycle detection via
// the inDFS set; already-visited stacks are skipped.
func (s *traversalState) loadStack(st *stack.Stack, ctx stackCtx) error {
	if s.visited[ctx.Identifier] && ctx.Identifier != "" {
		return nil
	}
	if s.inDFS[ctx.Identifier] && ctx.Identifier != "" {
		return fmt.Errorf("resolver: extends cycle detected at %q", ctx.Identifier)
	}
	s.inDFS[ctx.Identifier] = true
	defer func() {
		s.inDFS[ctx.Identifier] = false
		s.visited[ctx.Identifier] = true
	}()

	for i, ext := range st.Extends {
		if err := s.loadExtends(ext, ctx); err != nil {
			s.errs = append(s.errs, fmt.Errorf("stack %q extends[%d] (%q): %w", displayID(ctx.Identifier), i, ext.Raw, err))
		}
	}

	root := st.EffectiveRoot()
	for _, cat := range definitions.AllCategories {
		entries := st.EntriesFor(cat)
		for i, entry := range entries {
			w, err := s.resolveEntry(entry, cat, root, ctx)
			if err != nil {
				s.errs = append(s.errs, fmt.Errorf("stack %q %s[%d] (%q): %w", displayID(ctx.Identifier), cat.CategoryDir(), i, entry.Raw, err))
				continue
			}
			if w == nil {
				continue
			}
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
			s.overlay[key] = *w
		}
	}

	s.order = append(s.order, ctx.Identifier)
	return nil
}

// loadExtends resolves one extends entry (URL or path) and recurses.
func (s *traversalState) loadExtends(ext stack.ExtendsRef, parent stackCtx) error {
	switch ext.Kind {
	case stack.RefURL:
		repoURL, inRepoPath := splitGitURL(ext.URL)
		if inRepoPath == "" {
			return fmt.Errorf("URL extends must include an in-repo path after .git/ (got %q)", ext.URL)
		}
		childFS, rr, err := s.provider.Provide(sourceref.Source{URL: repoURL, Ref: ext.Ref})
		if err != nil {
			return fmt.Errorf("provider: %w", err)
		}
		if s.sources.add(repoURL, rr.Ref, rr.SHA, SourceStack) {
			s.diags = append(s.diags, Diagnostic{
				Kind:      DiagImplicitSource,
				Message:   fmt.Sprintf("source %s pulled in via stack extends from %q", repoURL, displayID(parent.Identifier)),
				SourceURL: repoURL,
				StackName: parent.Identifier,
			})
		}

		identifier := ext.URL + "@" + ext.Ref
		childStack, err := stack.ParseInFS(childFS, inRepoPath)
		if err != nil {
			return fmt.Errorf("parse stack: %w", err)
		}
		childCtx := stackCtx{
			Identifier:   identifier,
			SourceURL:    repoURL,
			SourceRef:    rr.Ref,
			FS:           childFS,
			FilePathInFS: inRepoPath,
		}
		return s.loadStack(childStack, childCtx)

	case stack.RefPath:
		childPath := joinFromFile(parent.FilePathInFS, ext.Path)
		identifier := parent.SourceURL + "@" + parent.SourceRef + ":" + childPath
		if parent.SourceURL == "" {
			identifier = "local:" + childPath
		}
		childStack, err := stack.ParseInFS(parent.FS, childPath)
		if err != nil {
			return fmt.Errorf("parse stack: %w", err)
		}
		childCtx := stackCtx{
			Identifier:   identifier,
			SourceURL:    parent.SourceURL,
			SourceRef:    parent.SourceRef,
			FS:           parent.FS,
			FilePathInFS: childPath,
		}
		return s.loadStack(childStack, childCtx)
	}
	return fmt.Errorf("unsupported extends kind %v", ext.Kind)
}

// resolveEntry loads one per-category entry into a walkedDef.
func (s *traversalState) resolveEntry(entry stack.EntryRef, cat definitions.Category, root string, ctx stackCtx) (*walkedDef, error) {
	switch entry.Kind {
	case stack.RefBare:
		return s.resolveBare(entry, cat, root, ctx)
	case stack.RefPath:
		return s.resolvePath(entry, cat, ctx)
	case stack.RefURL:
		return s.resolveURL(entry, cat, ctx)
	}
	return nil, fmt.Errorf("unsupported entry kind %v", entry.Kind)
}

// resolveBare looks up a bare name under <root>/<plural>/<name>... in
// the stack's source FS.
func (s *traversalState) resolveBare(entry stack.EntryRef, cat definitions.Category, root string, ctx stackCtx) (*walkedDef, error) {
	bundleDir, fileName, err := bareLayout(root, cat, entry.Name, ctx.FS)
	if err != nil {
		return nil, err
	}
	return s.parseFromFS(ctx.FS, bundleDir, fileName, cat, entry.Name, ctx)
}

// resolvePath resolves a ./relative entry against the stack file's
// parent directory.
func (s *traversalState) resolvePath(entry stack.EntryRef, cat definitions.Category, ctx stackCtx) (*walkedDef, error) {
	resolved := joinFromFile(ctx.FilePathInFS, entry.Path)
	bundleDir, fileName, err := pathLayout(cat, resolved)
	if err != nil {
		return nil, err
	}
	return s.parseFromFS(ctx.FS, bundleDir, fileName, cat, "", ctx)
}

// resolveURL resolves an external URL entry. The URL must contain
// `.git/` plus an in-repo path; the resolver fetches the repo, then
// parses the bundle/file at the in-repo path.
func (s *traversalState) resolveURL(entry stack.EntryRef, cat definitions.Category, ctx stackCtx) (*walkedDef, error) {
	repoURL, inRepoPath := splitGitURL(entry.URL)
	if inRepoPath == "" {
		return nil, fmt.Errorf("URL entry must include an in-repo path after .git/ (got %q)", entry.URL)
	}
	repoFS, rr, err := s.provider.Provide(sourceref.Source{URL: repoURL, Ref: entry.Ref})
	if err != nil {
		return nil, fmt.Errorf("provider: %w", err)
	}
	if s.sources.add(repoURL, rr.Ref, rr.SHA, SourceDefinition) {
		s.diags = append(s.diags, Diagnostic{
			Kind:      DiagImplicitSource,
			Message:   fmt.Sprintf("source %s pulled in via %s entry from stack %q", repoURL, cat.CategoryDir(), displayID(ctx.Identifier)),
			SourceURL: repoURL,
			StackName: ctx.Identifier,
		})
	}

	bundleDir, fileName, err := pathLayout(cat, inRepoPath)
	if err != nil {
		return nil, err
	}
	urlCtx := stackCtx{
		Identifier:   ctx.Identifier,
		SourceURL:    repoURL,
		SourceRef:    rr.Ref,
		FS:           repoFS,
		FilePathInFS: ctx.FilePathInFS,
	}
	return s.parseFromFS(repoFS, bundleDir, fileName, cat, "", urlCtx)
}

// parseFromFS reads the entry from rootFS at (bundleDir, fileName) and
// builds a walkedDef. For bundle categories fileName is the entry file
// (SKILL.md / AGENT.md) and bundleDir is the bundle directory itself.
// For file categories bundleDir is the parent directory and fileName is
// the file's basename.
//
// Empty bundleDir means rootFS is already at the right level — used for
// path-form entries that resolve to the FS root.
func (s *traversalState) parseFromFS(rootFS fs.FS, bundleDir, fileName string, cat definitions.Category, expectedName string, ctx stackCtx) (*walkedDef, error) {
	dirFS := rootFS
	if bundleDir != "" && bundleDir != "." {
		sub, err := fs.Sub(rootFS, bundleDir)
		if err != nil {
			return nil, fmt.Errorf("fs.Sub %q: %w", bundleDir, err)
		}
		dirFS = sub
	}

	var (
		def definitions.Definition
		err error
	)
	if isBundleCategory(cat) {
		name := expectedName
		if name == "" {
			name = path.Base(bundleDir)
		}
		def, err = definitions.ParseBundle(dirFS, cat, name)
	} else {
		def, err = definitions.ParseFile(dirFS, cat, fileName)
	}
	if err != nil {
		return nil, err
	}

	return &walkedDef{
		Category:   cat,
		Name:       def.GetCommon().Name,
		Definition: def,
		SourceURL:  ctx.SourceURL,
		SourceRef:  ctx.SourceRef,
		StackName:  ctx.Identifier,
		EntryPath:  fileName,
		SourceFS:   dirFS,
	}, nil
}

// ===== layout helpers =====

// bareLayout builds the (bundle dir, entry file) pair for a bare-name
// entry under <root>/<plural>/<name>. For hook/mcp/setting it tries
// .yaml then .yml.
func bareLayout(root string, cat definitions.Category, name string, fsys fs.FS) (bundleDir, fileName string, err error) {
	switch cat {
	case definitions.CategorySkill:
		return path.Join(root, "skills", name), "SKILL.md", nil
	case definitions.CategoryAgent:
		return path.Join(root, "agents", name), "AGENT.md", nil
	case definitions.CategoryRule:
		return path.Join(root, "rules"), name + ".md", nil
	case definitions.CategoryInstruction:
		return path.Join(root, "instructions"), name + ".md", nil
	case definitions.CategoryCommand:
		// Nested namespacing allowed: name may contain "/".
		dir, file := path.Split(name + ".md")
		return path.Join(root, "commands", dir), file, nil
	case definitions.CategoryHook:
		return path.Join(root, "hooks"), pickYAMLOrYML(fsys, path.Join(root, "hooks"), name), nil
	case definitions.CategoryMCP:
		return path.Join(root, "mcp"), pickYAMLOrYML(fsys, path.Join(root, "mcp"), name), nil
	case definitions.CategorySetting:
		return path.Join(root, "settings"), pickYAMLOrYML(fsys, path.Join(root, "settings"), name), nil
	}
	return "", "", fmt.Errorf("unsupported category %q", cat)
}

// pathLayout splits a resolved in-FS path into (bundle dir, file name).
// For bundle categories the resolved path IS the bundle dir; the entry
// file inside it is fixed (SKILL.md/AGENT.md). For file categories the
// resolved path is the file itself.
func pathLayout(cat definitions.Category, resolved string) (bundleDir, fileName string, err error) {
	if isBundleCategory(cat) {
		entry := "SKILL.md"
		if cat == definitions.CategoryAgent {
			entry = "AGENT.md"
		}
		return resolved, entry, nil
	}
	return path.Dir(resolved), path.Base(resolved), nil
}

func isBundleCategory(cat definitions.Category) bool {
	return cat == definitions.CategorySkill || cat == definitions.CategoryAgent
}

// pickYAMLOrYML returns "name.yaml" if it exists in dir, else "name.yml"
// if that exists, else "name.yaml" as a default (so the downstream parse
// produces a clean "no such file" rather than a missing-extension one).
func pickYAMLOrYML(fsys fs.FS, dir, name string) string {
	for _, ext := range []string{".yaml", ".yml"} {
		p := path.Join(dir, name+ext)
		if _, err := fs.Stat(fsys, p); err == nil {
			return name + ext
		}
	}
	return name + ".yaml"
}

// joinFromFile resolves a "./relative" path against the directory holding
// fileInFS. Both inputs are forward-slash, FS-style paths.
func joinFromFile(fileInFS, relative string) string {
	dir := path.Dir(fileInFS)
	if dir == "." {
		dir = ""
	}
	clean := strings.TrimPrefix(relative, "./")
	if dir == "" {
		return path.Clean(clean)
	}
	return path.Clean(path.Join(dir, clean))
}

// splitGitURL splits a URL on `.git/`, returning (repoURL, inRepoPath).
// If no `.git/` substring is present, returns (u, "").
func splitGitURL(u string) (repoURL, inRepoPath string) {
	const sep = ".git/"
	i := strings.Index(u, sep)
	if i < 0 {
		return u, ""
	}
	return u[:i+len(".git")], strings.TrimRight(u[i+len(sep):], "/")
}

// displayID renders an identifier for diagnostic messages. The empty
// string identifies the entry-point stack; rendered as "<entry>".
func displayID(id string) string {
	if id == "" {
		return "<entry>"
	}
	return id
}
