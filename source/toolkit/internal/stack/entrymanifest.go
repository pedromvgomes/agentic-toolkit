package stack

import (
	"bytes"
	"os"

	"github.com/goccy/go-yaml"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
)

// DefaultLocalRoot is the default value of `root:` when an entry manifest
// omits it — the convention root for locally-scanned definitions, distinct
// from Stack's DefaultRoot (the convention root for bare-name lookups).
const DefaultLocalRoot = "agentic"

// EntryManifest is the deserialised entry manifest (.agentic-toolkit.yaml).
// Its category fields mean "scan root/<category>/ by convention"; its
// `root:`, `context:`, `memory:` and `platforms:` fields are native to the
// entry manifest alone and do not exist on Stack, which composes shared
// content pulled in via `extends:` rather than a consumer's own content and
// local scanning root.
type EntryManifest struct {
	Description string `yaml:"description,omitempty" agtkdoc:"One-line summary of this repo's entry manifest."`
	Root        string `yaml:"root,omitempty"        agtkdoc:"Convention root for locally-scanned definitions, relative to the repo root. Defaults to \"agentic\"."`
	Context     string `yaml:"context,omitempty"     agtkdoc:"Path to one file of free-form repo description, rendered first as an instruction."`

	Stacks []ExtendsRef `yaml:"stacks,omitempty" agtkdoc:"Shared stacks to compose into this entry manifest. Applied in declared order; later entries override earlier ones. Each entry is an external URL (with .git/ boundary) or a local path (./...)."`

	Platforms []definitions.Platform `yaml:"platforms,omitempty" agtkdoc:"Rendering targets. Omit to render Claude Code only — today's behavior, unchanged. List additional platforms (e.g. codex) to also render their on-disk layout from the same definitions; each named platform must have a render adapter."`

	Memory *MemoryConfig `yaml:"memory,omitempty" agtkdoc:"Repo-resident memory store settings. The store's location is a fact about the consumer repo, not about a shareable stack."`
}

// MemoryRoot returns the configured store root, or "" when the entry
// manifest does not set one (the caller then applies the default).
func (m *EntryManifest) MemoryRoot() string {
	if m.Memory == nil {
		return ""
	}
	return m.Memory.Root
}

// MemoryAgent returns the configured curation provider, or "" when the
// entry manifest names none.
//
// There is deliberately no default. Every other memory command is
// deterministic and free; this is the one that spends money and calls out to
// a CLI, so a repo that has not chosen a provider gets a refusal rather than
// a guess about which one it meant.
func (m *EntryManifest) MemoryAgent() string {
	if m.Memory == nil {
		return ""
	}
	return m.Memory.Agent
}

// EffectiveRoot returns Root if set, else DefaultLocalRoot.
func (m *EntryManifest) EffectiveRoot() string {
	if m.Root == "" {
		return DefaultLocalRoot
	}
	return m.Root
}

// EffectivePlatforms returns Platforms if set, else a single-element slice
// naming Claude Code — omitting platforms: renders exactly what agtk has
// always rendered, with no behavior change for a manifest that never sets it.
func (m *EntryManifest) EffectivePlatforms() []definitions.Platform {
	if len(m.Platforms) == 0 {
		return []definitions.Platform{definitions.PlatformClaude}
	}
	return m.Platforms
}

// ParseEntryManifestFile reads and decodes an entry manifest from the local
// filesystem. Used by the CLI for the entry-point file (.agentic-toolkit.yaml).
func ParseEntryManifestFile(filePath string) (*EntryManifest, error) {
	raw, err := os.ReadFile(filePath) // #nosec G304 -- parses the entry manifest at the path the invoker named
	if err != nil {
		return nil, &ParseError{Path: filePath, Kind: ErrIO, Message: err.Error(), Wrapped: err}
	}
	return ParseEntryManifestBytes(filePath, raw)
}

// ParseEntryManifestBytes decodes raw YAML bytes into an EntryManifest. The
// path argument is only used in error messages; raw should be the entire
// file contents.
func ParseEntryManifestBytes(filePath string, raw []byte) (*EntryManifest, error) {
	if err := detectLegacyConfig(filePath, raw); err != nil {
		return nil, err
	}

	var m EntryManifest
	dec := yaml.NewDecoder(bytes.NewReader(raw), yaml.Strict())
	if err := dec.Decode(&m); err != nil {
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

	for i := range m.Stacks {
		ref, err := ParseExtendsRef(m.Stacks[i].Raw)
		if err != nil {
			return nil, newErr(filePath, ErrInvalidExtends,
				"stacks[%d]: %v", i, err)
		}
		m.Stacks[i] = ref
	}

	for _, p := range m.Platforms {
		if !definitions.IsKnownPlatform(p) {
			return nil, newErr(filePath, ErrUnknownPlatform,
				"unknown platform %q in platforms (known: %v)", p, definitions.AllPlatforms)
		}
	}

	return &m, nil
}
