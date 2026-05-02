package resolver

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/config"
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
)

// Resolve walks cfg's preset stack against the primary source (and any
// external sources reached via preset refs) and returns a Plan. On any
// failure during preset/definition resolution, errors are joined and no
// Plan is returned. A failure to provide the primary source is fatal and
// returned immediately; failures to provide a single declared external or
// to resolve a single preset entry are accumulated and the resolver
// continues.
func Resolve(cfg *config.ConsumerConfig, provider SourceProvider) (*Plan, error) {
	if cfg == nil {
		return nil, errors.New("resolver: nil ConsumerConfig")
	}
	if provider == nil {
		return nil, errors.New("resolver: nil SourceProvider")
	}

	// 1. Acquire primary. Failure is fatal: without it we can't load
	//    presets, so reporting any other error would be cascade noise.
	primaryFS, primaryRR, err := provider.Provide(cfg.Source)
	if err != nil {
		return nil, fmt.Errorf("resolver: primary source %q: %w", cfg.Source.URL, err)
	}

	srcs := newSourceTable()
	srcs.add(cfg.Source.URL, primaryRR.Ref, primaryRR.SHA, SourcePrimary)

	var errs []error

	// 2. Acquire each declared external. Failures collect; a missing
	//    declared external suppresses the sub-walk for that source but
	//    does not abort other sources.
	declaredFS := make(map[srcKey]fs.FS, len(cfg.Externals))
	declaredFailed := make(map[srcKey]bool, len(cfg.Externals))
	for _, ext := range cfg.Externals {
		k := srcKey{URL: ext.URL, Ref: ext.Ref}
		extFS, extRR, perr := provider.Provide(ext)
		if perr != nil {
			errs = append(errs, fmt.Errorf("resolver: declared external %q: %w", ext.URL, perr))
			declaredFailed[k] = true
			continue
		}
		declaredFS[k] = extFS
		srcs.add(ext.URL, extRR.Ref, extRR.SHA, SourceDeclared)
	}

	// 3. Walk presets in stacking order. Each entry produces an
	//    intermediate `walked` record; dedupe + ordering happen after.
	var walked []walkedDef
	var diags []Diagnostic

	for _, presetName := range cfg.Presets {
		preset, perr := readPreset(primaryFS, presetName)
		if perr != nil {
			errs = append(errs, fmt.Errorf("resolver: preset %q: %w", presetName, perr))
			continue
		}
		for i, refStr := range preset.Definitions {
			ref, rerr := definitions.ParsePresetRef(refStr)
			if rerr != nil {
				// ParsePresetInCatalog already validated each ref, so
				// reaching here means the preset file changed shape
				// underneath us. Surface it as an error against this
				// preset entry.
				errs = append(errs, fmt.Errorf("resolver: preset %q entry %d (%q): %w", presetName, i, refStr, rerr))
				continue
			}
			w, werr := walkRef(provider, primaryFS, cfg, ref, refStr, presetName, declaredFS, declaredFailed, srcs, &diags)
			if werr != nil {
				errs = append(errs, werr)
				continue
			}
			if w != nil {
				walked = append(walked, *w)
			}
		}
	}

	// 4. Dedupe by (Category, Name): last entry wins, losers emit
	//    DiagOverride. Walk order respects (preset order, in-preset
	//    entry order).
	winners := make(map[defKey]walkedDef)
	for _, w := range walked {
		k := defKey{Category: w.Category, Name: w.Name}
		if prev, ok := winners[k]; ok {
			diags = append(diags, Diagnostic{
				Kind: DiagOverride,
				Message: fmt.Sprintf("%s/%s from %s was overridden by entry from %s via preset %q",
					w.Category.CategoryDir(), w.Name, prev.SourceURL, w.SourceURL, w.PresetName),
				Category:   w.Category,
				Name:       w.Name,
				SourceURL:  prev.SourceURL,
				PresetName: w.PresetName,
			})
		}
		winners[k] = w
	}

	// 5. Materialize ordered Definitions.
	defs := make([]PlannedDefinition, 0, len(winners))
	for _, w := range winners {
		defs = append(defs, PlannedDefinition{
			Category:   w.Category,
			Name:       w.Name,
			Definition: w.Definition,
			SourceURL:  w.SourceURL,
			SourceRef:  w.SourceRef,
			PresetName: w.PresetName,
			EntryPath:  w.EntryPath,
		})
	}
	sort.Slice(defs, func(i, j int) bool {
		if defs[i].Category != defs[j].Category {
			return defs[i].Category < defs[j].Category
		}
		return defs[i].Name < defs[j].Name
	})

	// 6. Order sources: Primary, Declared (config order), Implicit
	//    (sorted by (URL, Ref)).
	plannedSources := srcs.ordered(cfg)

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &Plan{
		Config:      cfg,
		Sources:     plannedSources,
		Definitions: defs,
		Diagnostics: diags,
	}, nil
}

// walkedDef is the intermediate per-entry record built during the preset
// walk. Promoted to PlannedDefinition after dedupe.
type walkedDef struct {
	Category   definitions.Category
	Name       string
	Definition definitions.Definition
	SourceURL  string
	SourceRef  string
	PresetName string
	EntryPath  string
}

type defKey struct {
	Category definitions.Category
	Name     string
}

// walkRef resolves one preset entry against the appropriate source. It
// records sources/diagnostics into the shared srcs table and diags slice
// and returns either a walkedDef (success) or an error.
//
// nil, nil is returned when the entry was skipped silently (e.g. its
// declared external failed to provide and we suppressed cascading
// errors).
func walkRef(
	provider SourceProvider,
	primaryFS fs.FS,
	cfg *config.ConsumerConfig,
	ref definitions.PresetRef,
	refStr, presetName string,
	declaredFS map[srcKey]fs.FS,
	declaredFailed map[srcKey]bool,
	srcs *sourceTable,
	diags *[]Diagnostic,
) (*walkedDef, error) {
	if ref.IsExternal() {
		return walkExternal(provider, ref, refStr, presetName, declaredFS, declaredFailed, srcs, diags)
	}

	// Local ref: resolve against primary.
	path, err := localEntryPath(ref.Category, ref.Name, primaryFS)
	if err != nil {
		return nil, fmt.Errorf("resolver: preset %q entry %q: %w", presetName, refStr, err)
	}
	def, err := definitions.ParseInCatalog(primaryFS, path)
	if err != nil {
		return nil, fmt.Errorf("resolver: preset %q entry %q: %w", presetName, refStr, err)
	}
	return &walkedDef{
		Category:   ref.Category,
		Name:       ref.Name,
		Definition: def,
		SourceURL:  cfg.Source.URL,
		SourceRef:  cfg.Source.Ref,
		PresetName: presetName,
		EntryPath:  path,
	}, nil
}

// walkExternal handles an external preset ref. Bundle categories
// (skill, agent) call the provider with the bundle URL; file categories
// (rule, instruction, command, hook, mcp) lop the URL last segment off
// and call the provider with the parent URL, then ParseFile the file by
// name. Either way, the URL recorded in srcs and on the walkedDef is the
// URL the consumer wrote — bundle dir for bundles, file URL for files.
func walkExternal(
	provider SourceProvider,
	ref definitions.PresetRef,
	refStr, presetName string,
	declaredFS map[srcKey]fs.FS,
	declaredFailed map[srcKey]bool,
	srcs *sourceTable,
	diags *[]Diagnostic,
) (*walkedDef, error) {
	isBundle := ref.Category == definitions.CategorySkill || ref.Category == definitions.CategoryAgent

	var providerURL, filename, entryPath string
	if isBundle {
		providerURL = ref.URL
		if ref.Category == definitions.CategorySkill {
			entryPath = "SKILL.md"
		} else {
			entryPath = "AGENT.md"
		}
	} else {
		parent, last, ok := splitURLLastSeg(ref.URL)
		if !ok {
			return nil, fmt.Errorf("resolver: preset %q entry %q: external file ref must include a filename in the URL path (e.g. .git/path/to/file.md)", presetName, refStr)
		}
		providerURL = parent
		filename = last
		entryPath = last
	}

	// Classify against declared externals: exact (URL, Ref) match on the
	// URL the consumer wrote. File-URL declarations are unusual but the
	// match logic stays uniform; in practice file refs almost always
	// classify as Implicit.
	k := srcKey{URL: ref.URL, Ref: ref.Ref}
	var extFS fs.FS
	if fsys, isDeclared := declaredFS[k]; isDeclared {
		extFS = fsys
	} else if declaredFailed[k] {
		// Declared but its provider failed — suppress further processing
		// for this entry to avoid cascading "missing definition" noise on
		// top of the provider error.
		return nil, nil
	} else {
		fsys, rr, err := provider.Provide(config.Source{URL: providerURL, Ref: ref.Ref})
		if err != nil {
			return nil, fmt.Errorf("resolver: preset %q entry %q: provider: %w", presetName, refStr, err)
		}
		extFS = fsys
		if srcs.add(ref.URL, rr.Ref, rr.SHA, SourceImplicit) {
			*diags = append(*diags, Diagnostic{
				Kind:       DiagImplicitExternal,
				Message:    fmt.Sprintf("source %s pulled in implicitly via preset %q", ref.URL, presetName),
				SourceURL:  ref.URL,
				PresetName: presetName,
			})
		}
	}

	var (
		def definitions.Definition
		err error
	)
	if isBundle {
		name := lastURLSeg(ref.URL)
		def, err = definitions.ParseBundle(extFS, ref.Category, name)
	} else {
		def, err = definitions.ParseFile(extFS, ref.Category, filename)
	}
	if err != nil {
		return nil, fmt.Errorf("resolver: preset %q entry %q: %w", presetName, refStr, err)
	}

	return &walkedDef{
		Category:   ref.Category,
		Name:       def.GetCommon().Name,
		Definition: def,
		SourceURL:  ref.URL,
		SourceRef:  ref.Ref,
		PresetName: presetName,
		EntryPath:  entryPath,
	}, nil
}

// localEntryPath constructs the fs-relative entry-point path for a local
// preset ref. For hook/mcp it tries .yaml first and falls back to .yml.
func localEntryPath(cat definitions.Category, name string, fsys fs.FS) (string, error) {
	switch cat {
	case definitions.CategorySkill:
		return fmt.Sprintf("definitions/skills/%s/SKILL.md", name), nil
	case definitions.CategoryAgent:
		return fmt.Sprintf("definitions/agents/%s/AGENT.md", name), nil
	case definitions.CategoryRule:
		return fmt.Sprintf("definitions/rules/%s.md", name), nil
	case definitions.CategoryInstruction:
		return fmt.Sprintf("definitions/instructions/%s.md", name), nil
	case definitions.CategoryCommand:
		return fmt.Sprintf("definitions/commands/%s.md", name), nil
	case definitions.CategoryHook:
		return resolveYAMLOrYML(fsys, "definitions/hooks/"+name)
	case definitions.CategoryMCP:
		return resolveYAMLOrYML(fsys, "definitions/mcp/"+name)
	case definitions.CategorySetting:
		return resolveYAMLOrYML(fsys, "definitions/settings/"+name)
	}
	return "", fmt.Errorf("unsupported category %q", cat)
}

func resolveYAMLOrYML(fsys fs.FS, base string) (string, error) {
	for _, ext := range []string{".yaml", ".yml"} {
		p := base + ext
		if _, err := fs.Stat(fsys, p); err == nil {
			return p, nil
		}
	}
	// Default to .yaml so the downstream ParseInCatalog produces a clean
	// "no such file" error instead of a misleading missing-extension one.
	return base + ".yaml", nil
}

// readPreset locates and parses a preset by name in the primary source.
// Tries .yaml then .yml.
func readPreset(primaryFS fs.FS, name string) (*definitions.Preset, error) {
	for _, ext := range []string{".yaml", ".yml"} {
		path := definitions.PresetsDir + "/" + name + ext
		if _, err := fs.Stat(primaryFS, path); err != nil {
			continue
		}
		return definitions.ParsePresetInCatalog(primaryFS, path)
	}
	return nil, fmt.Errorf("not found in %s/%s.{yaml,yml}", definitions.PresetsDir, name)
}

// lastURLSeg returns the last "/"-separated segment of s. Used to derive
// canonical names for external bundle refs.
func lastURLSeg(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// splitURLLastSeg splits an external file ref URL into (parent, filename).
// Requires the URL to contain `.git/` followed by at least one path
// segment — i.e. the URL identifies an in-repo file. Returns ok=false
// otherwise.
func splitURLLastSeg(u string) (parent, last string, ok bool) {
	const sep = ".git/"
	i := strings.Index(u, sep)
	if i < 0 || i+len(sep) >= len(u) {
		return "", "", false
	}
	j := strings.LastIndex(u, "/")
	if j < 0 || j+1 >= len(u) {
		return "", "", false
	}
	return u[:j], u[j+1:], true
}
