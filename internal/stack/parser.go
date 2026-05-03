package stack

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
)

// ParseFile reads and decodes a stack manifest from the local filesystem.
// Used by the CLI for the entry-point file (.agentic-toolkit.yaml).
func ParseFile(filePath string) (*Stack, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, &ParseError{Path: filePath, Kind: ErrIO, Message: err.Error(), Wrapped: err}
	}
	return ParseBytes(filePath, raw)
}

// ParseInFS reads and decodes a stack manifest at fsPath inside fsys. Used
// by the resolver when loading stacks pulled in via `extends:`.
func ParseInFS(fsys fs.FS, fsPath string) (*Stack, error) {
	fsPath = path.Clean(filepath.ToSlash(fsPath))
	raw, err := fs.ReadFile(fsys, fsPath)
	if err != nil {
		return nil, &ParseError{Path: fsPath, Kind: ErrIO, Message: err.Error(), Wrapped: err}
	}
	return ParseBytes(fsPath, raw)
}

// ParseBytes decodes raw YAML bytes into a Stack. The path argument is
// only used in error messages; raw should be the entire file contents.
func ParseBytes(filePath string, raw []byte) (*Stack, error) {
	if err := detectLegacyConfig(filePath, raw); err != nil {
		return nil, err
	}

	var s Stack
	dec := yaml.NewDecoder(bytes.NewReader(raw), yaml.Strict())
	if err := dec.Decode(&s); err != nil {
		line, col := extractYAMLPos(err)
		kind := classifyYAMLError(err)
		return nil, &ParseError{
			Path:    filePath,
			Line:    line,
			Column:  col,
			Kind:    kind,
			Message: cleanYAMLMessage(err),
			Wrapped: err,
		}
	}

	for i := range s.Extends {
		ref, err := ParseExtendsRef(s.Extends[i].Raw)
		if err != nil {
			return nil, newErr(filePath, ErrInvalidExtends,
				"extends[%d]: %v", i, err)
		}
		s.Extends[i] = ref
	}

	for _, cat := range definitions.AllCategories {
		entries := s.entriesPtr(cat)
		if entries == nil {
			continue
		}
		for i := range *entries {
			parsed, err := ParseEntryRef((*entries)[i].Raw, cat)
			if err != nil {
				return nil, newErr(filePath, ErrInvalidEntry,
					"%s[%d]: %v", cat.CategoryDir(), i, err)
			}
			(*entries)[i] = parsed
		}
	}

	return &s, nil
}

// entriesPtr returns a pointer to the per-category slice on s, so the
// parser can mutate the parsed entries in place.
func (s *Stack) entriesPtr(cat definitions.Category) *[]EntryRef {
	switch cat {
	case definitions.CategorySkill:
		return &s.Skills
	case definitions.CategoryAgent:
		return &s.Agents
	case definitions.CategoryRule:
		return &s.Rules
	case definitions.CategoryInstruction:
		return &s.Instructions
	case definitions.CategoryCommand:
		return &s.Commands
	case definitions.CategoryHook:
		return &s.Hooks
	case definitions.CategoryMCP:
		return &s.MCP
	case definitions.CategorySetting:
		return &s.Settings
	}
	return nil
}

// UnmarshalYAML reads each entry as a string and stashes it in EntryRef.Raw.
// The full parse (Kind/Name/Path/URL/Ref) runs in ParseBytes after the
// strict-decode pass so we can attach category context to errors.
func (e *EntryRef) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return fmt.Errorf("entry must be a string: %w", err)
	}
	e.Raw = s
	return nil
}

// UnmarshalYAML reads each extends entry as a string. The full parse runs
// later; see EntryRef.UnmarshalYAML for the same pattern.
func (e *ExtendsRef) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return fmt.Errorf("extends entry must be a string: %w", err)
	}
	e.Raw = s
	return nil
}

// ParseEntryRef parses one per-category entry string into an EntryRef.
// Disambiguation, in order:
//
//   - starts with `./` or `/` → Path ref
//   - contains `.git/` → URL ref (with optional `@<ref>`)
//   - starts with a known provider host (github.com/, bitbucket.org/,
//     codeberg.org/) followed by at least owner/repo → URL ref, with the
//     `.git/` boundary inferred. Lets users write the natural form they
//     copy out of a browser.
//   - otherwise → Bare name
//
// `cat` is consulted only to decide whether '/' inside a bare name is
// permitted (commands accept nested namespacing; everything else is a
// single segment).
func ParseEntryRef(raw string, cat definitions.Category) (EntryRef, error) {
	if raw == "" {
		return EntryRef{}, fmt.Errorf("entry is empty")
	}
	out := EntryRef{Raw: raw}

	if strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "/") {
		out.Kind = RefPath
		out.Path = path.Clean(filepath.ToSlash(raw))
		if out.Path == "" || out.Path == "." {
			return EntryRef{}, fmt.Errorf("path entry %q resolves to empty", raw)
		}
		return out, nil
	}

	if strings.Contains(raw, gitBoundary) {
		url, ref := splitAtRef(raw)
		if url == "" {
			return EntryRef{}, fmt.Errorf("URL entry %q has empty url", raw)
		}
		out.Kind = RefURL
		out.URL = url
		out.Ref = ref
		return out, nil
	}

	if normalised, ok := inferProviderURL(raw); ok {
		url, ref := splitAtRef(normalised)
		out.Kind = RefURL
		out.URL = url
		out.Ref = ref
		return out, nil
	}

	if err := validateBareName(raw, cat); err != nil {
		return EntryRef{}, err
	}
	out.Kind = RefBare
	out.Name = raw
	return out, nil
}

// ParseExtendsRef parses one entry from a stack's `extends:` list. Only
// URL and Path forms are valid — bare names are rejected, since the
// convention root is for definition lookups, not stack imports.
//
// Provider auto-split applies the same way as ParseEntryRef: an entry
// like `github.com/owner/repo/stacks/foo.yaml` is accepted and the
// `.git/` boundary is inferred.
func ParseExtendsRef(raw string) (ExtendsRef, error) {
	if raw == "" {
		return ExtendsRef{}, fmt.Errorf("extends entry is empty")
	}
	out := ExtendsRef{Raw: raw}

	if strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "/") {
		out.Kind = RefPath
		out.Path = path.Clean(filepath.ToSlash(raw))
		if out.Path == "" || out.Path == "." {
			return ExtendsRef{}, fmt.Errorf("path extends entry %q resolves to empty", raw)
		}
		return out, nil
	}

	if strings.Contains(raw, gitBoundary) {
		url, ref := splitAtRef(raw)
		if url == "" {
			return ExtendsRef{}, fmt.Errorf("URL extends entry %q has empty url", raw)
		}
		out.Kind = RefURL
		out.URL = url
		out.Ref = ref
		return out, nil
	}

	if normalised, ok := inferProviderURL(raw); ok {
		url, ref := splitAtRef(normalised)
		out.Kind = RefURL
		out.URL = url
		out.Ref = ref
		return out, nil
	}

	if hint := providerURLHint(raw); hint != "" {
		return ExtendsRef{}, fmt.Errorf("extends entry %q looks like a URL but is missing the %q boundary. Did you mean %q? Bare names are not permitted in extends", raw, gitBoundary, hint)
	}
	return ExtendsRef{}, fmt.Errorf("extends entry %q must be a URL (containing %q) or a local path (./ or /); bare names are not permitted in extends", raw, gitBoundary)
}

// gitBoundary is the explicit separator between a git repository URL and
// an in-repo path. Mirrors internal/sourcestore/url.go.
const gitBoundary = ".git/"

// splitAtRef splits at the rightmost '@', returning (left, right). If no
// '@' is present, returns (s, "").
func splitAtRef(s string) (left, right string) {
	if i := strings.LastIndex(s, "@"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// validateBareName enforces per-category name shape on a bare entry name.
// Commands accept nested names like "group/cmd"; every other category
// must be a single segment with no '/' separators.
//
// When the input looks like it was meant to be a URL (host with a dot
// before the first '/'), the error includes a corrected suggestion so
// the user doesn't have to dig through docs to figure out what '.git/'
// is for.
func validateBareName(name string, cat definitions.Category) error {
	if name == "" {
		return fmt.Errorf("bare name is empty")
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return fmt.Errorf("bare name %q must not start or end with '/'", name)
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("bare name %q contains invalid segment %q", name, seg)
		}
	}
	if cat != definitions.CategoryCommand && strings.Contains(name, "/") {
		if _, ok := inferProviderURL(name); ok {
			// Should never reach here — ParseEntryRef auto-splits these
			// before validateBareName runs. Kept as a defensive guard.
			return fmt.Errorf("internal error: provider URL %q reached bare-name validation", name)
		}
		if hint := providerURLHint(name); hint != "" {
			return fmt.Errorf("%s name %q looks like a URL but is missing the %q boundary that separates the repo URL from the in-repo path. Add %q after the repo name — for example: %q", cat, name, gitBoundary, gitBoundary, hint)
		}
		return fmt.Errorf("%s name %q cannot contain '/'; only commands support nested names like 'group/cmd'", cat, name)
	}
	return nil
}

// knownProviderHosts lists the git hosts where the repo URL is
// conventionally exactly host/owner/repo. For these, agtk infers the
// `.git/` boundary so users can copy the form they see in a browser.
//
// gitlab.com is intentionally excluded because nested groups
// (gitlab.com/group/subgroup/repo) make owner/repo segment-counting
// ambiguous; gitlab users still need an explicit `.git/`.
var knownProviderHosts = []string{
	"github.com",
	"bitbucket.org",
	"codeberg.org",
}

// inferProviderURL returns a normalised URL — with an explicit `.git/`
// boundary inserted after owner/repo — when raw starts with a known
// provider host followed by at least owner/repo. The optional `@<ref>`
// suffix is preserved. Returns ("", false) for inputs that don't match.
func inferProviderURL(raw string) (string, bool) {
	base, ref := splitAtRef(raw)
	for _, host := range knownProviderHosts {
		prefix := host + "/"
		if !strings.HasPrefix(base, prefix) {
			continue
		}
		rest := base[len(prefix):]
		owner, afterOwner, hasOwner := strings.Cut(rest, "/")
		if !hasOwner || owner == "" {
			return "", false
		}
		repo, sub, _ := strings.Cut(afterOwner, "/")
		if repo == "" {
			return "", false
		}
		out := host + "/" + owner + "/" + repo + ".git"
		if sub != "" {
			out += "/" + sub
		}
		if ref != "" {
			out += "@" + ref
		}
		return out, true
	}
	return "", false
}

// providerURLHint returns a "did you mean…" suggestion for inputs that
// look like a URL but are missing the `.git/` boundary. For known
// providers we can build the exact corrected form via inferProviderURL;
// for any other host-shaped input (a '.' before the first '/') we fall
// back to a generic instruction.
func providerURLHint(raw string) string {
	if normalised, ok := inferProviderURL(raw); ok {
		return normalised
	}
	if firstSlash := strings.Index(raw, "/"); firstSlash > 0 {
		host := raw[:firstSlash]
		if strings.Contains(host, ".") {
			rest := raw[firstSlash+1:]
			owner, after, hasOwner := strings.Cut(rest, "/")
			if hasOwner && owner != "" {
				repo, sub, _ := strings.Cut(after, "/")
				if repo != "" && sub != "" {
					return host + "/" + owner + "/" + repo + ".git/" + sub
				}
			}
		}
	}
	return ""
}

// ===== legacy-format detection =====

// detectLegacyConfig returns a friendly error if the file looks like a v1
// consumer config or v1 preset (uses `source:`, `presets:`, `externals:`,
// or `definitions:` as a top-level field). Detection runs before strict
// YAML decode so users see a migration hint rather than an unknown-field
// error.
func detectLegacyConfig(filePath string, raw []byte) error {
	for _, key := range legacyTopLevelKeys {
		if topLevelKeyRE(key).Match(raw) {
			return newErr(filePath, ErrLegacyConfig,
				"%q is a v1 schema field; this is a stack manifest (v2). See docs/MIGRATION.md to upgrade.", key)
		}
	}
	return nil
}

var legacyTopLevelKeys = []string{"source", "presets", "externals", "definitions", "platforms"}

// topLevelKeyRE returns a regex matching `<key>:` at column zero of any
// line, ignoring lines inside YAML block scalars is not perfect — but the
// keys we look for never appear at column zero outside top-level position
// in a stack file.
func topLevelKeyRE(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `\s*:`)
}

// ===== YAML error helpers =====

func classifyYAMLError(err error) ErrorKind {
	msg := err.Error()
	if strings.Contains(msg, "unknown field") {
		return ErrUnknownField
	}
	return ErrYAMLSyntax
}

var yamlPosRE = regexp.MustCompile(`\[(\d+):(\d+)\]`)

func extractYAMLPos(err error) (int, int) {
	if se, ok := err.(*yaml.SyntaxError); ok {
		if t := se.Token; t != nil && t.Position != nil {
			return t.Position.Line, t.Position.Column
		}
	}
	m := yamlPosRE.FindStringSubmatch(err.Error())
	if len(m) == 3 {
		var l, c int
		fmt.Sscanf(m[1], "%d", &l)
		fmt.Sscanf(m[2], "%d", &c)
		return l, c
	}
	return 0, 0
}

func cleanYAMLMessage(err error) string {
	s := err.Error()
	if i := strings.Index(s, "\n"); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
