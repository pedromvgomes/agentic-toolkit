// Package stack models a stack manifest — the unified replacement for the
// old `consumer config` and `preset` concepts.
//
// A stack is a single YAML file with these fields:
//
//	description: optional, used by tooling when this stack is imported
//	root:        optional, default "definitions"; convention root for bare-name lookups
//	extends:     other stacks to layer under this one (URL or ./path)
//	skills:      []EntryRef
//	agents:      []EntryRef
//	rules:       []EntryRef
//	instructions:[]EntryRef
//	commands:    []EntryRef
//	hooks:       []EntryRef
//	mcp:         []EntryRef
//	settings:    []EntryRef
//
// Override semantics: depth-first walk of `extends:`, post-order overlay
// (children apply before importer's own entries), entry-point file's
// entries win last. Same (category, name) pair = later wins.
//
// Per-entry shape (string in YAML, parsed into EntryRef):
//
//   - contains `.git/` → URL ref (external git source)
//   - starts with `./` or `/` → Path ref (local to this file's repo)
//   - otherwise → Bare name (resolved under <root>/<plural>/<name>...)
//
// The same shape is used for the consumer's .agentic-toolkit.yaml and for
// shareable stacks at stacks/<name>.yaml in any repo. There is no "preset"
// vs "consumer config" distinction: the consumer's file is just an
// entry-point stack.
package stack

//go:generate go run ../../tools/schemagen

import (
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
)

// DefaultRoot is the default value of `root:` when a stack file omits it.
// Used by the resolver to compute bare-name lookup paths.
const DefaultRoot = "definitions"

// Stack is the deserialised stack manifest. It carries one EntryRef slice
// per definition category. The order in which entries appear in each
// slice is preserved; later entries within the same stack file win on
// (category, name) collisions.
type Stack struct {
	Description string       `yaml:"description,omitempty" agtkdoc:"One-line summary used by tooling when this stack is imported by another stack."`
	Root        string       `yaml:"root,omitempty"        agtkdoc:"Convention root for bare-name lookups, relative to the stack file's repo root. Defaults to \"definitions\"."`
	Extends     []ExtendsRef `yaml:"extends,omitempty"     agtkdoc:"Other stacks to layer under this one. Applied in declared order; later entries override earlier ones. Each entry is an external URL (with .git/ boundary) or a local path (./...)."`

	Skills       []EntryRef `yaml:"skills,omitempty"`
	Agents       []EntryRef `yaml:"agents,omitempty"`
	Rules        []EntryRef `yaml:"rules,omitempty"`
	Instructions []EntryRef `yaml:"instructions,omitempty"`
	Commands     []EntryRef `yaml:"commands,omitempty"`
	Hooks        []EntryRef `yaml:"hooks,omitempty"`
	MCP          []EntryRef `yaml:"mcp,omitempty"`
	Settings     []EntryRef `yaml:"settings,omitempty"`
}

// EffectiveRoot returns Root if set, else DefaultRoot.
func (s *Stack) EffectiveRoot() string {
	if s.Root == "" {
		return DefaultRoot
	}
	return s.Root
}

// EntriesFor returns the EntryRef slice for cat. Returns nil for unknown
// categories rather than panicking — the parser already validates categories.
func (s *Stack) EntriesFor(cat definitions.Category) []EntryRef {
	switch cat {
	case definitions.CategorySkill:
		return s.Skills
	case definitions.CategoryAgent:
		return s.Agents
	case definitions.CategoryRule:
		return s.Rules
	case definitions.CategoryInstruction:
		return s.Instructions
	case definitions.CategoryCommand:
		return s.Commands
	case definitions.CategoryHook:
		return s.Hooks
	case definitions.CategoryMCP:
		return s.MCP
	case definitions.CategorySetting:
		return s.Settings
	}
	return nil
}

// RefKind classifies the shape of one entry in a stack's per-category list
// or its `extends:` list.
type RefKind int

const (
	// RefBare names a definition by short name; the resolver looks it up
	// under <root>/<plural>/<name>... in the stack file's own repo.
	RefBare RefKind = iota
	// RefPath points at a local file or bundle directory in the stack
	// file's repo, ignoring `root:`. Always starts with "./" or "/".
	RefPath
	// RefURL points at an external git source, identified by the `.git/`
	// boundary in the URL. Carries an optional `@<ref>` git ref.
	RefURL
)

func (k RefKind) String() string {
	switch k {
	case RefBare:
		return "bare"
	case RefPath:
		return "path"
	case RefURL:
		return "url"
	}
	return "unknown"
}

// EntryRef is one parsed entry from a stack's per-category list. Parser
// only validates shape; loading/fetching is the resolver's responsibility.
type EntryRef struct {
	// Raw is the verbatim string from the YAML, kept for diagnostics.
	Raw string

	// Kind disambiguates the three resolution paths.
	Kind RefKind

	// Name is set for RefBare. For commands the value may contain '/'
	// separators (nested namespacing); for every other category Name is a
	// single segment.
	Name string

	// Path is set for RefPath, normalised (cleaned, leading ./ stripped
	// is *not* applied — keep authored form for diagnostics).
	Path string

	// URL is set for RefURL — the URL through and including any in-repo
	// path past the `.git/` boundary, i.e. the full external locator.
	URL string

	// Ref is set for RefURL when the user supplied `@<ref>`. Empty means
	// "default branch".
	Ref string
}

// IsExternal reports whether this entry is an external URL ref.
func (e EntryRef) IsExternal() bool { return e.Kind == RefURL }

// ExtendsRef is one parsed entry from a stack's `extends:` list. It points
// at another stack manifest file, by URL or local path. Bare names are not
// permitted in `extends:` — the convention root is for definition lookups,
// not stack imports.
type ExtendsRef struct {
	// Raw is the verbatim string from the YAML.
	Raw string

	// Kind is RefURL or RefPath.
	Kind RefKind

	// Path is set for RefPath.
	Path string

	// URL is set for RefURL.
	URL string

	// Ref is set for RefURL when the user supplied `@<ref>`.
	Ref string
}

// IsExternal reports whether this extends ref is an external URL ref.
func (e ExtendsRef) IsExternal() bool { return e.Kind == RefURL }

// StacksDir is the canonical (forward-slash) location of shareable stack
// files inside a repo. Used by tooling when scanning a source for the
// stacks it publishes.
const StacksDir = "stacks"
