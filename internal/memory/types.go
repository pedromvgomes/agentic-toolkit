// Package memory implements the repo-resident memory store: durable notes
// about a codebase, each anchored to the content it was derived from.
//
// The store is committed to the consumer repo, so notes are reviewable in
// PRs and travel with the branch that wrote them. It holds three things:
//
//	INDEX.md      generated; the only file an agent always loads
//	notes/        one fact per file, markdown + YAML frontmatter
//	candidates/   staging area for findings a curator has not promoted yet
//
// Two independent axes describe a note, and conflating them is the mistake
// this package is built to avoid:
//
//   - Confidence is the curator's judgment about whether the claim was
//     checked. Only a curator writes it.
//   - Staleness is mechanical: the anchored files no longer hash to the
//     recorded blobs. It is recomputed from the working tree on every
//     audit and never stored, so it cannot drift from the code.
package memory

import "strings"

// Layout of a store, relative to its root.
const (
	// DefaultRoot is where the store lives when `memory.root` is unset,
	// relative to the directory holding the entry stack manifest.
	DefaultRoot = ".agents/memory"

	IndexFile     = "INDEX.md"
	NotesDir      = "notes"
	CandidatesDir = "candidates"

	// HitsFile records one line per `agtk memory show`. Local telemetry,
	// gitignored by the scaffold so it never becomes PR noise.
	HitsFile = ".hits.jsonl"

	GitignoreFile = ".gitignore"

	// GitkeepFile keeps the otherwise-empty candidates/ directory alive
	// through a clone.
	GitkeepFile = ".gitkeep"

	// NoteExt is the extension of a note file; the stem is its name.
	NoteExt = ".md"
)

// Kind classifies what a note records. The set is closed: anything that
// does not fit one of the four is re-derivable and does not belong in the
// store.
type Kind string

const (
	// KindInvariant states something that must stay true.
	KindInvariant Kind = "invariant"
	// KindRationale explains why a surprising thing is the way it is.
	KindRationale Kind = "rationale"
	// KindGotcha warns about a trap that is not visible from the code.
	KindGotcha Kind = "gotcha"
	// KindDeadEnd records an approach already tried and abandoned.
	KindDeadEnd Kind = "dead-end"
)

// AllKinds is the closed set, in the order a report lists them.
var AllKinds = []Kind{KindInvariant, KindRationale, KindGotcha, KindDeadEnd}

// Valid reports whether k is one of AllKinds.
func (k Kind) Valid() bool {
	for _, c := range AllKinds {
		if k == c {
			return true
		}
	}
	return false
}

// Confidence is the curator's verdict on a note's claim. It says nothing
// about whether the anchored files have changed since — that is staleness,
// and it is derived, not stored.
type Confidence string

const (
	// ConfidenceVerified means a curator checked the claim against the code.
	ConfidenceVerified Confidence = "verified"
	// ConfidenceSuspect means a curator kept the note without confirming it.
	ConfidenceSuspect Confidence = "suspect"
)

// AllConfidences is the closed set.
var AllConfidences = []Confidence{ConfidenceVerified, ConfidenceSuspect}

// Valid reports whether c is one of AllConfidences.
func (c Confidence) Valid() bool {
	for _, v := range AllConfidences {
		if c == v {
			return true
		}
	}
	return false
}

// Note is one fact, parsed from one file under notes/.
//
// Field order is the on-disk frontmatter order: rewriting a note marshals
// this struct, so the order here is what a reviewer sees in the diff.
type Note struct {
	Name        string     `yaml:"name"`
	Kind        Kind       `yaml:"kind"`
	Description string     `yaml:"description"`
	Anchors     []Anchor   `yaml:"anchors"`
	Confidence  Confidence `yaml:"confidence"`

	// Body is everything after the closing frontmatter delimiter. Rewrites
	// preserve it byte-for-byte; only frontmatter is regenerated.
	Body string `yaml:"-"`

	// File is the absolute path the note was read from.
	File string `yaml:"-"`
}

// Anchor ties a claim to the content it was derived from. Path is either a
// concrete file or a glob; the two carry their hashes differently:
//
//   - concrete path: Blob holds that file's hash.
//   - glob: Matches holds one entry per file the pattern expanded to, so a
//     file added to or removed from the pattern is detectable — a note most
//     often stops holding because something new appeared, which a single
//     hash over the set could flag but never name.
//
// Paths are slash-separated and relative to the project root (the directory
// holding the entry stack manifest), not to the store.
type Anchor struct {
	Path    string  `yaml:"path"`
	Blob    string  `yaml:"blob,omitempty"`
	Matches []Match `yaml:"matches,omitempty"`
}

// Match is one file a glob anchor expanded to, with its hash at stamp time.
type Match struct {
	Path string `yaml:"path"`
	Blob string `yaml:"blob"`
}

// IsGlob reports whether the anchor's path is a pattern rather than a
// concrete file.
func (a Anchor) IsGlob() bool {
	return strings.ContainsAny(a.Path, "*?[")
}
