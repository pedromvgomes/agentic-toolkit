package claude

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// wholeOp is one planned write of a whole-owned file. Bundle copies
// expand into one wholeOp per file.
type wholeOp struct {
	// RelPath is forward-slash-relative to ScopeRoot. Used as the
	// manifest key.
	RelPath string
	// AbsPath is the destination on disk.
	AbsPath string
	// Content is the bytes to write.
	Content []byte
}

// planWholeOwned builds the list of every whole-owned-file write the
// plan will perform: skill/agent entry files + their bundle companions,
// command files, rule files. Order is deterministic: alphabetical by
// RelPath.
func planWholeOwned(plan *resolver.Plan, roots scopeRoots) ([]wholeOp, error) {
	var ops []wholeOp
	for _, d := range plan.Definitions {
		switch d.Category {
		case definitions.CategorySkill:
			built, err := buildBundleOps(d, roots, "skills", "SKILL.md", renderSkill)
			if err != nil {
				return nil, err
			}
			ops = append(ops, built...)
		case definitions.CategoryAgent:
			built, err := buildBundleOps(d, roots, "agents", "AGENT.md", renderAgent)
			if err != nil {
				return nil, err
			}
			ops = append(ops, built...)
		case definitions.CategoryCommand:
			content, err := renderCommand(d.Definition)
			if err != nil {
				return nil, err
			}
			rel := path.Join("commands", d.Name+".md")
			ops = append(ops, wholeOp{
				RelPath: rel,
				AbsPath: filepath.Join(roots.ScopeRoot, filepath.FromSlash(rel)),
				Content: content,
			})
		case definitions.CategoryRule:
			content, err := renderRule(d.Definition)
			if err != nil {
				return nil, err
			}
			rel := path.Join("rules", d.Name+".md")
			ops = append(ops, wholeOp{
				RelPath: rel,
				AbsPath: filepath.Join(roots.ScopeRoot, filepath.FromSlash(rel)),
				Content: content,
			})
		}
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].RelPath < ops[j].RelPath })
	return ops, nil
}

// buildBundleOps emits the entry-file write plus a verbatim copy of
// every companion file under path.Dir(EntryPath) in the SourceFS.
func buildBundleOps(
	d resolver.PlannedDefinition,
	roots scopeRoots,
	dirName, entryFilename string,
	renderEntry func(definitions.Definition) ([]byte, error),
) ([]wholeOp, error) {
	entryContent, err := renderEntry(d.Definition)
	if err != nil {
		return nil, err
	}
	bundleRel := path.Join(dirName, d.Name)
	ops := []wholeOp{{
		RelPath: path.Join(bundleRel, entryFilename),
		AbsPath: filepath.Join(roots.ScopeRoot, dirName, d.Name, entryFilename),
		Content: entryContent,
	}}
	if d.SourceFS == nil {
		// Defensive: no SourceFS means we can't copy companions. The
		// resolver always populates SourceFS, so this is unreachable in
		// practice. Skip companions silently rather than fail.
		return ops, nil
	}
	bundleDir := path.Dir(d.EntryPath)
	if bundleDir == "." {
		bundleDir = ""
	}
	walkRoot := bundleDir
	if walkRoot == "" {
		walkRoot = "."
	}
	werr := fs.WalkDir(d.SourceFS, walkRoot, func(p string, dirent fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if dirent.IsDir() {
			return nil
		}
		// Skip the entry file (already rendered above from parsed
		// Definition). Compare against the absolute entry path that the
		// resolver recorded.
		if p == d.EntryPath {
			return nil
		}
		rel := strings.TrimPrefix(p, bundleDir)
		rel = strings.TrimPrefix(rel, "/")
		raw, rerr := fs.ReadFile(d.SourceFS, p)
		if rerr != nil {
			return rerr
		}
		ops = append(ops, wholeOp{
			RelPath: path.Join(bundleRel, rel),
			AbsPath: filepath.Join(roots.ScopeRoot, dirName, d.Name, filepath.FromSlash(rel)),
			Content: raw,
		})
		return nil
	})
	if werr != nil {
		return nil, fmt.Errorf("claude: walk bundle %s/%s: %w", dirName, d.Name, werr)
	}
	return ops, nil
}

// detectCollisions returns one error per existing-but-unmanaged target.
func detectCollisions(ops []wholeOp, manifest manifestState) []error {
	var errs []error
	for _, op := range ops {
		if _, tracked := manifest.Files[op.RelPath]; tracked {
			continue
		}
		if _, err := os.Stat(op.AbsPath); err == nil {
			errs = append(errs, fmt.Errorf("claude: %s exists and is not tracked by agtk; rerun with --force to overwrite", op.AbsPath))
		}
	}
	return errs
}

// applyWholeOp writes one op, updating newManifest with its hash.
// Idempotent: identical existing content is skipped (still tracked).
func applyWholeOp(op wholeOp, newManifest manifestState, opts Options) error {
	hash := contentHash(op.Content)
	newManifest.Files[op.RelPath] = hash

	existing, err := os.ReadFile(op.AbsPath)
	if err == nil && string(existing) == string(op.Content) {
		if opts.Stdout != nil {
			fmt.Fprintf(opts.Stdout, "unchanged %s\n", op.AbsPath)
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(op.AbsPath), 0o755); err != nil {
		return fmt.Errorf("claude: mkdir %s: %w", filepath.Dir(op.AbsPath), err)
	}
	if err := os.WriteFile(op.AbsPath, op.Content, 0o644); err != nil {
		return fmt.Errorf("claude: write %s: %w", op.AbsPath, err)
	}
	if opts.Stdout != nil {
		fmt.Fprintf(opts.Stdout, "wrote %s\n", op.AbsPath)
	}
	return nil
}

// ===== per-category entry rendering =====

func renderSkill(def definitions.Definition) ([]byte, error) {
	s := def.(*definitions.Skill)
	type fm struct {
		Name         string   `yaml:"name"`
		Description  string   `yaml:"description"`
		AllowedTools []string `yaml:"allowed-tools,omitempty"`
	}
	out := fm{Name: s.Name, Description: s.Description}
	if s.Extensions.Claude != nil {
		out.AllowedTools = s.Extensions.Claude.AllowedTools
	}
	return frontmatterPlusBody(out, s.Body)
}

func renderAgent(def definitions.Definition) ([]byte, error) {
	a := def.(*definitions.Agent)
	type fm struct {
		Name            string   `yaml:"name"`
		Description     string   `yaml:"description"`
		Model           string   `yaml:"model,omitempty"`
		Tools           []string `yaml:"tools,omitempty"`
		Color           string   `yaml:"color,omitempty"`
		DisallowedTools []string `yaml:"disallowed-tools,omitempty"`
		PermissionMode  string   `yaml:"permission-mode,omitempty"`
		MaxTurns        int      `yaml:"max-turns,omitempty"`
		Memory          string   `yaml:"memory,omitempty"`
		Background      bool     `yaml:"background,omitempty"`
		Effort          string   `yaml:"effort,omitempty"`
		Isolation       string   `yaml:"isolation,omitempty"`
		InitialPrompt   string   `yaml:"initial-prompt,omitempty"`
	}
	out := fm{
		Name:        a.Name,
		Description: a.Description,
		Model:       a.Model,
		Tools:       a.Tools,
		Color:       string(a.Color),
	}
	if a.Extensions.Claude != nil {
		ext := a.Extensions.Claude
		out.DisallowedTools = ext.DisallowedTools
		out.PermissionMode = ext.PermissionMode
		out.MaxTurns = ext.MaxTurns
		out.Memory = ext.Memory
		out.Background = ext.Background
		out.Effort = ext.Effort
		out.Isolation = ext.Isolation
		out.InitialPrompt = ext.InitialPrompt
	}
	return frontmatterPlusBody(out, a.Body)
}

func renderCommand(def definitions.Definition) ([]byte, error) {
	c := def.(*definitions.Command)
	type fm struct {
		Name         string   `yaml:"name"`
		Description  string   `yaml:"description"`
		ArgumentHint string   `yaml:"argument-hint,omitempty"`
		Model        string   `yaml:"model,omitempty"`
		Tools        []string `yaml:"tools,omitempty"`
	}
	out := fm{
		Name:         c.Name,
		Description:  c.Description,
		ArgumentHint: c.ArgumentHint,
		Model:        c.Model,
		Tools:        c.Tools,
	}
	return frontmatterPlusBody(out, c.Body)
}

func renderRule(def definitions.Definition) ([]byte, error) {
	r := def.(*definitions.Rule)
	type fm struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description,omitempty"`
	}
	out := fm{Name: r.Name, Description: r.Description}
	return frontmatterPlusBody(out, r.Body)
}

// frontmatterPlusBody serializes a typed frontmatter struct and appends
// the markdown body. Output ends with a single trailing newline.
func frontmatterPlusBody(frontmatter any, body string) ([]byte, error) {
	yamlBytes, err := yaml.Marshal(frontmatter)
	if err != nil {
		return nil, fmt.Errorf("claude: marshal frontmatter: %w", err)
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.Write(yamlBytes)
	b.WriteString("---\n")
	if body != "" {
		if !strings.HasPrefix(body, "\n") {
			b.WriteString("\n")
		}
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
	}
	return []byte(b.String()), nil
}

// ===== manifest =====

const manifestFileName = ".agtk-manifest.json"
const manifestVersion = 1

type manifestState struct {
	Version int               `json:"version"`
	Files   map[string]string `json:"files"`
}

func newManifestState() manifestState {
	return manifestState{Version: manifestVersion, Files: map[string]string{}}
}

func readManifest(scopeRoot string) (manifestState, error) {
	path := filepath.Join(scopeRoot, manifestFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newManifestState(), nil
		}
		return manifestState{}, fmt.Errorf("claude: read manifest %s: %w", path, err)
	}
	var m manifestState
	if err := json.Unmarshal(raw, &m); err != nil {
		return manifestState{}, fmt.Errorf("claude: parse manifest %s: %w", path, err)
	}
	if m.Files == nil {
		m.Files = map[string]string{}
	}
	return m, nil
}

func writeManifest(scopeRoot string, m manifestState) error {
	if err := os.MkdirAll(scopeRoot, 0o755); err != nil {
		return fmt.Errorf("claude: mkdir %s: %w", scopeRoot, err)
	}
	path := filepath.Join(scopeRoot, manifestFileName)
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("claude: marshal manifest: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("claude: write manifest %s: %w", path, err)
	}
	return nil
}

func contentHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// reportDryRun prints what each file's intended action would be without
// touching disk.
func reportDryRun(stdout interface{ Write(p []byte) (int, error) }, plan *resolver.Plan, ops []wholeOp, roots scopeRoots, manifest manifestState) {
	if stdout == nil {
		return
	}
	for _, op := range ops {
		existing, err := os.ReadFile(op.AbsPath)
		switch {
		case err != nil:
			fmt.Fprintf(stdout, "would write %s\n", op.AbsPath)
		case string(existing) == string(op.Content):
			fmt.Fprintf(stdout, "unchanged %s\n", op.AbsPath)
		default:
			fmt.Fprintf(stdout, "would update %s\n", op.AbsPath)
		}
	}
	for relPath := range manifest.Files {
		stillTracked := false
		for _, op := range ops {
			if op.RelPath == relPath {
				stillTracked = true
				break
			}
		}
		if !stillTracked {
			fmt.Fprintf(stdout, "would remove %s\n", filepath.Join(roots.ScopeRoot, relPath))
		}
	}
	// Settings + CLAUDE.md preview. Cheap but accurate enough: just
	// announce the targets — actual diff would require running the
	// merge logic without writing.
	hasInstr := false
	hasSettings := false
	for _, d := range plan.Definitions {
		switch d.Category {
		case definitions.CategoryInstruction:
			hasInstr = true
		case definitions.CategoryHook, definitions.CategoryMCP, definitions.CategorySetting:
			hasSettings = true
		}
	}
	if hasInstr {
		fmt.Fprintf(stdout, "would update %s (managed region)\n", instructionsPath(roots))
	}
	if hasSettings {
		fmt.Fprintf(stdout, "would update %s (managed top-level keys)\n", settingsPath(roots))
	}
}
