package definitions

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// Preset is a named bundle of definition references. Presets are toolkit-side
// metadata: they are not renderable, do not implement the Definition
// interface, and are not in the Category enum. Consumers select presets by
// name in their config; the resolver expands each preset's Definitions list
// against the available sources.
type Preset struct {
	Name        string   `yaml:"name,omitempty"  agtkdoc:"Optional. If present, must equal the filename stem."`
	Description string   `yaml:"description"     agtkdoc:"required;One-line summary used in tooling and discovery."`
	Definitions []string `yaml:"definitions"     agtkdoc:"required;Ordered list of definition refs. Local form: 'skills/foo' (or 'commands/git/commit' for nested). External form: 'skills::github.com/owner/repo/path[@ref]'."`
}

// PresetRef is the parsed form of one entry in Preset.Definitions.
//
// For local refs, Category and Name are set; URL and Ref are empty.
// For external refs, Category and URL are set; Ref is optional (empty means
// "default branch"); Name is empty (the resolver derives it from the URL
// path).
type PresetRef struct {
	Category Category
	Name     string
	URL      string
	Ref      string
}

// IsExternal reports whether this ref points to an external source.
func (r PresetRef) IsExternal() bool { return r.URL != "" }

// ParsePresetRef parses one entry from a preset's Definitions list.
//
// Local form: "<plural-dir>/<name>" — e.g. "skills/challenge",
// "commands/git/commit".
// External form: "<plural-dir>::<url>[@<ref>]" — e.g.
// "skills::github.com/anthropics/skills/skills/skill-creator@main".
//
// The parser validates format only. It does not crack URLs into
// host/owner/repo, classify the ref (branch/tag/sha), or verify that the
// referenced definition exists — those are the resolver's responsibility.
func ParsePresetRef(s string) (PresetRef, error) {
	if s == "" {
		return PresetRef{}, fmt.Errorf("empty preset ref")
	}
	if i := strings.Index(s, "::"); i >= 0 {
		dir := s[:i]
		rest := s[i+2:]
		cat := CategoryFromDir(dir)
		if cat == "" {
			return PresetRef{}, fmt.Errorf("unknown category %q in external preset ref %q", dir, s)
		}
		if rest == "" {
			return PresetRef{}, fmt.Errorf("external preset ref %q has empty URL after %q::", s, dir)
		}
		url, ref := splitAtRef(rest)
		if url == "" {
			return PresetRef{}, fmt.Errorf("external preset ref %q has empty URL", s)
		}
		return PresetRef{Category: cat, URL: url, Ref: ref}, nil
	}
	i := strings.Index(s, "/")
	if i < 0 {
		return PresetRef{}, fmt.Errorf("preset ref %q must contain '/' (local) or '::' (external)", s)
	}
	dir := s[:i]
	name := s[i+1:]
	cat := CategoryFromDir(dir)
	if cat == "" {
		return PresetRef{}, fmt.Errorf("unknown category %q in preset ref %q", dir, s)
	}
	if name == "" {
		return PresetRef{}, fmt.Errorf("preset ref %q has empty name", s)
	}
	return PresetRef{Category: cat, Name: name}, nil
}

// splitAtRef splits at the rightmost '@', returning (left, right).
// If no '@' is present, returns (s, "").
//
// HTTPS-style repository URLs (github.com/owner/repo/...) do not contain '@';
// SSH-style URLs (git@github.com:owner/repo) are not supported in slice-1.
func splitAtRef(s string) (left, right string) {
	if i := strings.LastIndex(s, "@"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// ParsePresetFile parses a single preset YAML file. The canonical name is
// derived from the filename stem.
func ParsePresetFile(path string) (*Preset, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &ParseError{Path: path, Kind: ErrIO, Message: err.Error(), Wrapped: err}
	}
	derivedName := stripExt(filepath.Base(path))
	return parsePresetBytes(path, derivedName, raw)
}

// ParsePresetInCatalog parses a preset file at path inside a catalog rooted
// at root. The path must live directly under definitions/presets/ — nesting
// is rejected.
func ParsePresetInCatalog(root, path string) (*Preset, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, &ParseError{Path: path, Kind: ErrIO, Message: err.Error(), Wrapped: err}
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, &ParseError{Path: path, Kind: ErrIO, Message: err.Error(), Wrapped: err}
	}
	presetsDir := filepath.Join(absRoot, "definitions", "presets")
	rel, err := filepath.Rel(presetsDir, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, newErr(path, ErrInvalidName,
			"path is not inside %s", presetsDir)
	}
	if strings.Contains(filepath.ToSlash(rel), "/") {
		return nil, newErr(path, ErrInvalidName,
			"presets must be flat (got nested path %q)", rel)
	}
	raw, err := os.ReadFile(absPath)
	if err != nil {
		return nil, &ParseError{Path: path, Kind: ErrIO, Message: err.Error(), Wrapped: err}
	}
	derivedName := stripExt(filepath.Base(absPath))
	return parsePresetBytes(path, derivedName, raw)
}

func parsePresetBytes(path, derivedName string, raw []byte) (*Preset, error) {
	var p Preset
	dec := yaml.NewDecoder(bytes.NewReader(raw), yaml.Strict())
	if err := dec.Decode(&p); err != nil {
		line, col := extractYAMLPos(err)
		kind := classifyYAMLError(err)
		return nil, &ParseError{
			Path:    path,
			Line:    line,
			Column:  col,
			Kind:    kind,
			Message: cleanYAMLMessage(err),
			Wrapped: err,
		}
	}
	if p.Description == "" {
		return nil, newErr(path, ErrMissingRequired, "description is required")
	}
	if p.Name == "" {
		p.Name = derivedName
	} else if p.Name != derivedName {
		return nil, newErr(path, ErrInvalidName,
			"name %q does not match path-derived name %q", p.Name, derivedName)
	}
	if len(p.Definitions) == 0 {
		return nil, newErr(path, ErrMissingRequired,
			"definitions list is required and must be non-empty")
	}
	for i, ref := range p.Definitions {
		if _, err := ParsePresetRef(ref); err != nil {
			return nil, newErr(path, ErrPresetMalformedRef,
				"definitions[%d]: %v", i, err)
		}
	}
	return &p, nil
}

// WalkPresets walks definitions/presets/ under root and returns one path per
// preset entry-point file. A missing presets/ directory is not an error.
func WalkPresets(root string) ([]string, error) {
	presetsDir := filepath.Join(root, "definitions", "presets")
	if _, err := os.Stat(presetsDir); os.IsNotExist(err) {
		return nil, nil
	}
	var out []string
	err := filepath.WalkDir(presetsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		out = append(out, path)
		return nil
	})
	return out, err
}
