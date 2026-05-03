// Package resolver walks a parsed stack manifest's extends DAG against a
// SourceProvider and produces a Plan: every source that was touched, every
// definition that resolved, and any diagnostics worth surfacing.
//
// Stack model:
//
//   - The entry-point file is a stack manifest (.agentic-toolkit.yaml in a
//     consumer repo, or any stack file passed to the resolver in tests).
//   - Each stack has an `extends:` list of other stacks (URL or local path),
//     applied in declared order; depth-first post-order overlay means
//     children's entries apply before the importing stack's own entries.
//   - Per-category lists (skills, agents, …) hold EntryRefs in three
//     shapes: bare name (resolved under <root>/<plural>/<name>... in the
//     stack file's source), local path (./… relative to that source), and
//     external URL (with `.git/` boundary).
//   - Override semantics: same (category, name) pair = later wins. The
//     entry-point's own entries always win last.
//
// The lockfile pins every URL the resolver touched: stack files reached
// via `extends:` and external definition sources reached via per-category
// URL entries.
package resolver

import (
	"io/fs"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/lockfile"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
)

// Plan is the resolver's primary output. It is in-memory only — see
// Plan.Lockfile for the persisted projection.
type Plan struct {
	// Stack is the entry-point stack manifest that produced this plan.
	// Adapters that need ordering information consume StackOrder; Stack
	// itself is exposed mostly for diagnostics and round-trip use.
	Stack *stack.Stack

	// StackOrder is the depth-first post-order list of stack identifiers
	// the resolver visited. Index 0 is the deepest-first child; the last
	// entry is the entry-point itself (identifier ""). Used by adapters
	// for last-wins tiebreaking on contributions from different stacks
	// (e.g. settings.json top-level keys).
	StackOrder []string

	// Sources is every source the resolver touched, in the deterministic
	// order: stacks first (in visit order), then definition sources sorted
	// by (URL, Ref).
	Sources []PlannedSource

	// Definitions is the deduped, ordered list of definitions to render.
	// One entry per (Category, Name); ordering is alphabetical by
	// (Category, Name). Override losers do not appear here — see
	// Diagnostics for who lost to whom.
	Definitions []PlannedDefinition

	// Diagnostics captures non-fatal observations: dedupe overrides and
	// implicit-source classifications. Empty when nothing of note
	// happened.
	Diagnostics []Diagnostic
}

// SourceKind classifies how a source ended up in the plan.
type SourceKind int

const (
	// SourceStack is a stack manifest source pulled in via `extends:`.
	SourceStack SourceKind = iota
	// SourceDefinition is a source reached via a per-category URL entry
	// in some stack.
	SourceDefinition
)

func (k SourceKind) String() string {
	switch k {
	case SourceStack:
		return "stack"
	case SourceDefinition:
		return "definition"
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
	// from. For external entries this is the URL the consumer wrote;
	// for local/bare entries it is the URL of the stack file's source
	// (or empty when loaded from the entry-point's local FS).
	SourceURL string
	SourceRef string

	// StackName identifies the stack whose entry caused this definition
	// to win the dedupe pass. For external stacks, this is the URL+ref
	// (matching an entry in Plan.StackOrder); for the entry-point file,
	// it is the empty string.
	StackName string

	// EntryPath is the fs-relative path inside the source's filesystem to
	// the entry-point file that was parsed. For local refs this is
	// "<root>/<plural>/<name>..." in the stack's source FS. For external
	// bundle refs (skill, agent) this is "SKILL.md" or "AGENT.md" — the
	// source filesystem is rooted at the bundle directory itself. For
	// external file refs this is the filename basename.
	EntryPath string

	// SourceFS is the filesystem the entry was parsed from. Adapters
	// consume this for bundle companion-file copy: walk
	// path.Dir(EntryPath) and copy every file except EntryPath itself.
	// For file categories there is nothing to copy; the field is still
	// populated for uniformity.
	SourceFS fs.FS
}

// DiagnosticKind enumerates the structured diagnostics the resolver emits.
type DiagnosticKind int

const (
	// DiagOverride: a (category, name) collision was resolved by
	// "last entry wins". The losing entry's source is recorded for
	// transparency.
	DiagOverride DiagnosticKind = iota
	// DiagImplicitSource: an entry pulled in a source that is not the
	// stack's own source. The implicit source is locked like any other;
	// the diagnostic is purely informational.
	DiagImplicitSource
)

func (k DiagnosticKind) String() string {
	switch k {
	case DiagOverride:
		return "override"
	case DiagImplicitSource:
		return "implicit_source"
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
	// PlannedDefinition.SourceURL). For DiagImplicitSource it is the
	// implicit source's URL.
	SourceURL string

	// StackName is the stack whose entry generated this diagnostic.
	StackName string
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
