package definitions

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
)

// ParseFile parses a single definition file. cat selects the target struct
// type. Use this when the caller already knows the category (tests, or a
// resolver that has indexed definitions some other way).
func ParseFile(path string, cat Category) (Definition, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &ParseError{Path: path, Kind: ErrIO, Message: err.Error(), Wrapped: err}
	}
	derivedName, err := deriveName(cat, path, "")
	if err != nil {
		return nil, err
	}
	return parseBytes(path, cat, derivedName, raw)
}

// ParseInCatalog parses a definition file at path inside a catalog rooted
// at root. The category is derived from the path (root/<category-dir>/...)
// and the canonical name is derived from the path layout per category rules.
func ParseInCatalog(root, path string) (Definition, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, &ParseError{Path: path, Kind: ErrIO, Message: err.Error(), Wrapped: err}
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, &ParseError{Path: path, Kind: ErrIO, Message: err.Error(), Wrapped: err}
	}
	defsDir := filepath.Join(absRoot, "definitions")
	rel, err := filepath.Rel(defsDir, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, newErr(path, ErrUnknownCategory,
			"path is not inside %s", defsDir)
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return nil, newErr(path, ErrUnknownCategory,
			"expected definitions/<category>/...")
	}
	cat := CategoryFromDir(parts[0])
	if cat == "" {
		return nil, newErr(path, ErrUnknownCategory,
			"unknown category directory %q", parts[0])
	}
	relWithinCat := filepath.ToSlash(filepath.Join(parts[1:]...))
	raw, err := os.ReadFile(absPath)
	if err != nil {
		return nil, &ParseError{Path: path, Kind: ErrIO, Message: err.Error(), Wrapped: err}
	}
	derivedName, err := deriveName(cat, absPath, relWithinCat)
	if err != nil {
		// Replace the path on the error with the user-facing path.
		if pe, ok := err.(*ParseError); ok {
			pe.Path = path
		}
		return nil, err
	}
	return parseBytes(path, cat, derivedName, raw)
}

// parseBytes is the shared core: classify file shape, run the decode, run
// validation, attach the body for prose categories.
func parseBytes(path string, cat Category, derivedName string, raw []byte) (Definition, error) {
	def := newDef(cat)
	if def == nil {
		return nil, newErr(path, ErrUnknownCategory, "no struct registered for category %q", cat)
	}

	var (
		yamlBytes []byte
		body      []byte
	)

	if isMarkdownCategory(cat) {
		fm, b, err := splitFrontmatter(path, raw)
		if err != nil {
			return nil, err
		}
		yamlBytes = fm
		body = b
	} else {
		yamlBytes = raw
	}

	if err := strictDecode(path, yamlBytes, def); err != nil {
		return nil, err
	}

	if isMarkdownCategory(cat) {
		setBody(def, string(body))
	}

	if err := validateCommon(path, def, derivedName); err != nil {
		return nil, err
	}
	if err := def.validate(path); err != nil {
		return nil, err
	}
	return def, nil
}

// newDef returns a freshly-allocated struct pointer for cat.
func newDef(cat Category) Definition {
	switch cat {
	case CategorySkill:
		return &Skill{}
	case CategoryRule:
		return &Rule{}
	case CategoryInstruction:
		return &Instruction{}
	case CategoryAgent:
		return &Agent{}
	case CategoryCommand:
		return &Command{}
	case CategoryHook:
		return &Hook{}
	case CategoryMCP:
		return &MCPServer{}
	}
	return nil
}

func isMarkdownCategory(cat Category) bool {
	switch cat {
	case CategorySkill, CategoryRule, CategoryInstruction, CategoryAgent, CategoryCommand:
		return true
	}
	return false
}

// setBody assigns the markdown body to definitions that carry one.
func setBody(def Definition, body string) {
	switch d := def.(type) {
	case *Skill:
		d.Body = body
	case *Rule:
		d.Body = body
	case *Instruction:
		d.Body = body
	case *Agent:
		d.Body = body
	case *Command:
		d.Body = body
	}
}

// ===== frontmatter =====

var frontmatterDelim = regexp.MustCompile(`(?m)^---[[:space:]]*\r?\n`)

func splitFrontmatter(path string, raw []byte) (yaml, body []byte, err error) {
	// Must start with "---\n" (allowing optional CRLF and trailing whitespace).
	first := frontmatterDelim.FindIndex(raw)
	if first == nil || first[0] != 0 {
		return nil, nil, newErr(path, ErrFrontmatterMissing,
			"file must begin with a YAML frontmatter block delimited by ---")
	}
	rest := raw[first[1]:]
	close := frontmatterDelim.FindIndex(rest)
	if close == nil {
		return nil, nil, newErr(path, ErrFrontmatterUnclosed,
			"frontmatter opening --- is not followed by a closing ---")
	}
	yaml = rest[:close[0]]
	body = rest[close[1]:]
	return yaml, body, nil
}

// ===== strict YAML decode =====

func strictDecode(path string, src []byte, into interface{}) error {
	dec := yaml.NewDecoder(bytes.NewReader(src), yaml.Strict())
	if err := dec.Decode(into); err != nil {
		line, col := extractYAMLPos(err)
		kind := classifyYAMLError(err)
		return &ParseError{
			Path:    path,
			Line:    line,
			Column:  col,
			Kind:    kind,
			Message: cleanYAMLMessage(err),
			Wrapped: err,
		}
	}
	return nil
}

// classifyYAMLError maps goccy/go-yaml errors to our error kinds.
// Strict mode emits unknown-field errors with a recognizable message.
func classifyYAMLError(err error) ErrorKind {
	msg := err.Error()
	if strings.Contains(msg, "unknown field") {
		return ErrUnknownField
	}
	return ErrYAMLSyntax
}

// extractYAMLPos pulls a line/column from goccy errors when present. The
// library's syntax errors expose Token().Position; we read from the message
// as a fallback to avoid taking a hard dependency on internal types.
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

// cleanYAMLMessage strips repeated source-context dumps that goccy adds so
// the ParseError message stays readable. The Wrapped error keeps the full
// original.
func cleanYAMLMessage(err error) string {
	s := err.Error()
	if i := strings.Index(s, "\n"); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// ===== name derivation =====

func deriveName(cat Category, path, relWithinCat string) (string, error) {
	if relWithinCat == "" {
		// caller didn't supply a category-relative path — derive from
		// the absolute path's tail using category-specific rules.
		switch cat {
		case CategorySkill:
			// .../skills/<name>/SKILL.md → name = parent dir
			return filepath.Base(filepath.Dir(path)), nil
		case CategoryAgent, CategoryRule, CategoryInstruction, CategoryHook, CategoryMCP:
			return stripExt(filepath.Base(path)), nil
		case CategoryCommand:
			// Without a category-relative path we can only derive the leaf.
			return stripExt(filepath.Base(path)), nil
		}
		return "", newErr(path, ErrUnknownCategory, "category %q not supported", cat)
	}
	switch cat {
	case CategorySkill:
		// <name>/SKILL.md
		parts := strings.Split(relWithinCat, "/")
		if len(parts) != 2 || strings.ToUpper(parts[1]) != "SKILL.MD" {
			return "", newErr(path, ErrInvalidName,
				"skills must live at <name>/SKILL.md (got %q)", relWithinCat)
		}
		return parts[0], nil
	case CategoryAgent, CategoryRule, CategoryInstruction:
		// flat <name>.md
		if strings.Contains(relWithinCat, "/") {
			return "", newErr(path, ErrInvalidName,
				"%s definitions must be flat (got nested path %q)", cat, relWithinCat)
		}
		return stripExt(relWithinCat), nil
	case CategoryHook, CategoryMCP:
		// flat <name>.yaml
		if strings.Contains(relWithinCat, "/") {
			return "", newErr(path, ErrInvalidName,
				"%s definitions must be flat (got nested path %q)", cat, relWithinCat)
		}
		return stripExt(relWithinCat), nil
	case CategoryCommand:
		// nesting allowed — full path under commands/, joined with /
		return stripExt(relWithinCat), nil
	}
	return "", newErr(path, ErrUnknownCategory, "category %q not supported", cat)
}

func stripExt(name string) string {
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext)
}

// ===== Common-level validation =====

func validateCommon(path string, def Definition, derivedName string) error {
	c := def.GetCommon()

	if c.Description == "" {
		return newErr(path, ErrMissingRequired, "description is required")
	}

	if c.Name == "" {
		c.Name = derivedName
	} else if c.Name != derivedName {
		return newErr(path, ErrInvalidName,
			"name %q does not match path-derived name %q", c.Name, derivedName)
	}

	for _, p := range c.Platforms {
		if !IsKnownPlatform(p) {
			return newErr(path, ErrUnknownPlatform,
				"unknown platform %q (known: %v)", p, AllPlatforms)
		}
	}

	if len(c.Platforms) > 0 {
		if err := validateExtensionsAgainstPlatforms(path, def); err != nil {
			return err
		}
	}
	return nil
}

// validateExtensionsAgainstPlatforms enforces Q12: when platforms is set
// explicitly, populated extension blocks must reference a listed platform.
func validateExtensionsAgainstPlatforms(path string, def Definition) error {
	listed := map[Platform]bool{}
	for _, p := range def.GetCommon().Platforms {
		listed[p] = true
	}
	for plat, present := range presentExtensions(def) {
		if present && !listed[plat] {
			return newErr(path, ErrPlatformExtension,
				"extensions.%s is set but platforms list (%v) does not include %s — add %s to platforms or drop the extension",
				plat, def.GetCommon().Platforms, plat, plat)
		}
	}
	return nil
}

// presentExtensions returns a map[platform]isPopulated for each category's
// extension struct. Done by switch rather than reflection to keep the
// schema definition (struct shape) the only source of truth.
func presentExtensions(def Definition) map[Platform]bool {
	m := map[Platform]bool{}
	switch d := def.(type) {
	case *Skill:
		m[PlatformClaude] = d.Extensions.Claude != nil
	case *Rule:
		m[PlatformCursor] = d.Extensions.Cursor != nil
	case *Agent:
		m[PlatformClaude] = d.Extensions.Claude != nil
		m[PlatformCursor] = d.Extensions.Cursor != nil
		m[PlatformOpenCode] = d.Extensions.OpenCode != nil
	case *Command:
		m[PlatformOpenCode] = d.Extensions.OpenCode != nil
		m[PlatformCopilot] = d.Extensions.Copilot != nil
	case *Hook:
		m[PlatformClaude] = d.Extensions.Claude != nil
		m[PlatformCursor] = d.Extensions.Cursor != nil
	case *MCPServer:
		m[PlatformClaude] = d.Extensions.Claude != nil
		m[PlatformOpenCode] = d.Extensions.OpenCode != nil
	}
	return m
}

// ===== category-specific validate methods =====

func (s *Skill) validate(path string) error { return nil }

func (r *Rule) validate(path string) error { return nil }

func (i *Instruction) validate(path string) error { return nil }

func (a *Agent) validate(path string) error {
	if a.Color != "" {
		ok := false
		for _, c := range AllAgentColors {
			if c == a.Color {
				ok = true
				break
			}
		}
		if !ok {
			return newErr(path, ErrUnknownColor,
				"color %q is not one of %v", a.Color, AllAgentColors)
		}
	}
	return nil
}

func (c *Command) validate(path string) error { return nil }

func (h *Hook) validate(path string) error {
	if h.Event == "" {
		return newErr(path, ErrMissingRequired, "event is required")
	}
	if h.Handler.Type == "" {
		return newErr(path, ErrMissingRequired, "handler.type is required")
	}
	ok := false
	for _, t := range AllHandlerTypes {
		if t == h.Handler.Type {
			ok = true
			break
		}
	}
	if !ok {
		return newErr(path, ErrUnknownHandler,
			"handler.type %q is not one of %v", h.Handler.Type, AllHandlerTypes)
	}
	switch h.Handler.Type {
	case HandlerCommand:
		if h.Handler.Command == "" {
			return newErr(path, ErrHandlerShape,
				"handler.command is required when handler.type is %q", HandlerCommand)
		}
		if h.Handler.Prompt != "" {
			return newErr(path, ErrHandlerShape,
				"handler.prompt must be empty when handler.type is %q", HandlerCommand)
		}
	case HandlerPrompt:
		if h.Handler.Prompt == "" {
			return newErr(path, ErrHandlerShape,
				"handler.prompt is required when handler.type is %q", HandlerPrompt)
		}
		if h.Handler.Command != "" {
			return newErr(path, ErrHandlerShape,
				"handler.command must be empty when handler.type is %q", HandlerPrompt)
		}
	}
	return nil
}

func (m *MCPServer) validate(path string) error {
	if m.Transport == "" {
		return newErr(path, ErrMissingRequired, "transport is required")
	}
	ok := false
	for _, t := range AllTransports {
		if t == m.Transport {
			ok = true
			break
		}
	}
	if !ok {
		return newErr(path, ErrUnknownTransport,
			"transport %q is not one of %v", m.Transport, AllTransports)
	}
	switch m.Transport {
	case TransportStdio:
		if m.Command == "" {
			return newErr(path, ErrMissingRequired,
				"command is required when transport is %q", TransportStdio)
		}
		if m.URL != "" || len(m.Headers) > 0 || m.OAuth != nil {
			return newErr(path, ErrTransportConflict,
				"http/sse fields (url, headers, oauth) are not allowed when transport is %q", TransportStdio)
		}
	case TransportHTTP, TransportSSE:
		if m.URL == "" {
			return newErr(path, ErrMissingRequired,
				"url is required when transport is %q", m.Transport)
		}
		if m.Command != "" || len(m.Args) > 0 || len(m.Env) > 0 {
			return newErr(path, ErrTransportConflict,
				"stdio fields (command, args, env) are not allowed when transport is %q", m.Transport)
		}
	}
	return nil
}
