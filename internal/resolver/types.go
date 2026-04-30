// Package resolver walks a parsed consumer config's preset stack against
// the primary toolkit source (and any external sources reached via preset
// refs) and produces a Plan: every source that was touched, every
// definition that resolved, and any diagnostics worth surfacing to the
// caller.
//
// External-ref shapes:
//   - Bundle categories (skill, agent) are folder-shaped: the preset
//     ref URL points to the bundle directory and the parser reads the
//     fixed entry file (SKILL.md / AGENT.md) from inside.
//   - File categories (rule, instruction, command, hook, mcp) are
//     file-shaped: the preset ref URL points to the file itself
//     (extension included). The resolver lops the URL last segment
//     off, asks the provider for the parent directory, and parses the
//     named file. The canonical name comes from the file's frontmatter
//     `name:` field (with filename-stem fallback) — remote layout is
//     irrelevant.
//
// The lockfile projection records every source touched, in the
// deterministic order: primary, then declared externals (config
// order), then implicit externals (sorted by URL,Ref). Each external
// preset ref produces one source entry keyed on the URL the consumer
// wrote — bundle dir for bundles, file URL for files.
//
// Out of scope: Common.Requires expansion, render-time platform
// filtering.
package resolver

import (
	"github.com/pedromvgomes/agentic-toolkit/internal/config"
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/lockfile"
)

// Plan is the resolver's primary output. It is in-memory only — see
// Plan.Lockfile for the persisted projection.
type Plan struct {
	// Config is the consumer config that produced this plan. Adapters
	// read Config.Platforms to apply per-platform filtering at render
	// time; the resolver itself does not filter by platform.
	Config *config.ConsumerConfig

	// Sources is every source the resolver touched, in the deterministic
	// order: Primary first, then Declared externals in config order, then
	// Implicit externals sorted by (URL, Ref).
	Sources []PlannedSource

	// Definitions is the deduped, ordered list of definitions to render.
	// One entry per (Category, Name); ordering is alphabetical by
	// (Category, Name). Override losers do not appear here — see
	// Diagnostics for who lost to whom.
	Definitions []PlannedDefinition

	// Diagnostics captures non-fatal observations: dedupe overrides and
	// implicit-external classifications. Empty when nothing of note
	// happened.
	Diagnostics []Diagnostic
}

// SourceKind classifies how a source ended up in the plan.
type SourceKind int

const (
	// SourcePrimary is the consumer config's primary source.
	SourcePrimary SourceKind = iota
	// SourceDeclared is an external listed in config.Externals.
	SourceDeclared
	// SourceImplicit is an external pulled in via a preset ref that
	// was not declared in config.Externals.
	SourceImplicit
)

func (k SourceKind) String() string {
	switch k {
	case SourcePrimary:
		return "primary"
	case SourceDeclared:
		return "declared"
	case SourceImplicit:
		return "implicit"
	}
	return "unknown"
}

// PlannedSource is a fully-pinned source entry in the plan.
type PlannedSource struct {
	URL  string
	Ref  string
	SHA  string
	Kind SourceKind
}

// PlannedDefinition is a single resolved definition ready for downstream
// rendering. The parsed Definition carries every category-specific field;
// SourceURL/SourceRef key into Plan.Sources for provenance.
type PlannedDefinition struct {
	Category   definitions.Category
	Name       string
	Definition definitions.Definition

	// SourceURL/SourceRef identify the source the definition was loaded
	// from. They are denormalized — adapters do not need to dereference
	// into Plan.Sources for the basic case.
	SourceURL string
	SourceRef string

	// PresetName is the preset whose entry caused this definition to win
	// the dedupe pass.
	PresetName string

	// EntryPath is the fs-relative path inside the source's filesystem to
	// the entry-point file that was parsed. For local refs this is
	// "definitions/<category>/<name>...". For external bundle refs (skill,
	// agent) this is "SKILL.md" or "AGENT.md" — the source filesystem is
	// rooted at the bundle directory itself.
	EntryPath string
}

// DiagnosticKind enumerates the structured diagnostics the resolver
// emits.
type DiagnosticKind int

const (
	// DiagOverride: a (category, name) collision was resolved by
	// "last preset wins". The losing entry's source is recorded for
	// transparency.
	DiagOverride DiagnosticKind = iota
	// DiagImplicitExternal: an external preset ref pulled in a source
	// that was not in config.Externals. The implicit source is locked
	// like any other; the diagnostic is purely informational.
	DiagImplicitExternal
)

func (k DiagnosticKind) String() string {
	switch k {
	case DiagOverride:
		return "override"
	case DiagImplicitExternal:
		return "implicit_external"
	}
	return "unknown"
}

// Diagnostic is a structured non-fatal observation. The Message field
// always carries a human-readable summary; the typed fields let CLIs and
// tests branch without parsing strings.
type Diagnostic struct {
	Kind    DiagnosticKind
	Message string

	// Category and Name are populated for DiagOverride.
	Category definitions.Category
	Name     string

	// SourceURL is populated for both kinds. For DiagOverride it is the
	// loser's source URL (the winner is reflected in
	// PlannedDefinition.SourceURL). For DiagImplicitExternal it is the
	// implicit source's URL.
	SourceURL string

	// PresetName is the preset whose entry generated this diagnostic.
	PresetName string
}

// Lockfile projects the plan to its persisted form.
func (p *Plan) Lockfile() *lockfile.Lockfile {
	out := &lockfile.Lockfile{Version: lockfile.Version}
	out.Sources = make([]lockfile.ResolvedSource, 0, len(p.Sources))
	for _, s := range p.Sources {
		out.Sources = append(out.Sources, lockfile.ResolvedSource{
			URL: s.URL,
			Ref: s.Ref,
			SHA: s.SHA,
		})
	}
	return out
}
