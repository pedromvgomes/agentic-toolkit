package reviewrun

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// Severity is how much a finding matters.
type Severity string

const (
	// SeverityRed means it must be fixed before merge, and is the default
	// approval floor.
	SeverityRed Severity = "RED"
	// SeverityAmber means it should be addressed but does not block.
	SeverityAmber Severity = "AMBER"
	// SeverityGreen is a remark rather than a defect.
	SeverityGreen Severity = "GREEN"
)

// Severities are the severities a finding can carry, ordered most serious
// first, which is the order a triage table lists them in.
var Severities = []Severity{SeverityRed, SeverityAmber, SeverityGreen}

// Rank orders severities so a set can be sorted without a table at each call
// site. Lower is more serious.
func (s Severity) Rank() int {
	for i, known := range Severities {
		if known == s {
			return i
		}
	}
	// An unrecognised severity sorts last rather than first. A judge that
	// invents one has said something the ladder does not describe, and the
	// safe reading of "I do not know how bad this is" is not "worst".
	return len(Severities)
}

// Valid reports whether s is on the ladder.
func (s Severity) Valid() bool { return s.Rank() < len(Severities) }

// Finding is one issue a reviewer reports.
//
// Line is a range because a defect is often a hunk rather than a statement,
// and it is optional because a claim about a subsystem has no line to attach
// to — that finding goes in the review body rather than inline.
type Finding struct {
	// ID is what the judge answers with. Assigned by agtk, meaningless
	// outside one run, and never persisted: identity across runs is the
	// fingerprint. See ADR 0008.
	ID string `json:"id"`
	// Reviewer is the manifest name of the runner that produced it.
	Reviewer string `json:"reviewer"`
	// Path is the file, relative to the repo root.
	Path string `json:"path"`
	// StartLine and EndLine bound the region, or are nil for a finding with
	// no line.
	StartLine *int `json:"start_line"`
	EndLine   *int `json:"end_line"`
	// Category is the kind of problem, e.g. "correctness" or
	// "security:prompt-injection".
	Category string `json:"category"`
	// Severity is the reviewer's own reading, which the judge may change.
	Severity Severity `json:"severity"`
	// Confidence is how sure the reviewer is.
	Confidence string `json:"confidence"`
	// Issue is the claim, in prose.
	Issue string `json:"issue"`
	// Evidence is the quoted code. It is what identity is computed from, so
	// it is carried forward byte for byte and never re-emitted by a later
	// run.
	Evidence string `json:"evidence"`
	// Suggestion is what to do about it, and may be empty.
	Suggestion string `json:"suggestion"`

	// Corroboration is how many distinct reviewer instances reached this
	// finding independently.
	Corroboration int `json:"corroboration"`
	// Verdict is what a validator made of it, and is empty when none ran.
	Verdict *Verdict `json:"verdict,omitempty"`
}

// Verdict is one validator's answer about one finding.
type Verdict struct {
	Verdict  string   `json:"verdict"`
	Severity Severity `json:"severity"`
	Reason   string   `json:"reason"`
}

// Validator verdicts.
const (
	VerdictUpheld     = "upheld"
	VerdictRejected   = "rejected"
	VerdictDowngraded = "downgraded"
)

// Upheld reports whether a finding survives validation. A finding nobody
// validated survives: the absence of a validator is not a rejection.
func (f Finding) Upheld() bool {
	return f.Verdict == nil || f.Verdict.Verdict != VerdictRejected
}

// HasLine reports whether this finding can become an inline comment. GitHub
// requires a path and a line, so one without either belongs in the review
// body.
func (f Finding) HasLine() bool { return f.StartLine != nil }

// normaliseEvidence reduces a quote to what identity is computed from: the
// first non-empty line, trimmed, with internal whitespace collapsed.
//
// The first line rather than the whole quote, because a reviewer that quotes
// one line where another quoted three is describing the same defect, and
// identity has to survive that. Whitespace is collapsed because re-indentation
// is not a different bug.
func normaliseEvidence(evidence string) string {
	for _, line := range strings.Split(evidence, "\n") {
		if trimmed := strings.Join(strings.Fields(line), " "); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// identity is what decides that two candidate findings are the same claim:
// the file, the kind of problem, and the code quoted.
//
// Deliberately not the line, which moves on every push, and not the issue
// prose, which is generated and differs between two runs describing one bug.
type identity struct {
	path     string
	category string
	evidence string
}

func (f Finding) identity() identity {
	return identity{
		path:     f.Path,
		category: f.Category,
		evidence: normaliseEvidence(f.Evidence),
	}
}

// FingerprintVersion is the scheme's version, carried in a posted marker.
//
// Load-bearing. A change to what is hashed makes every existing marker
// mismatch, and without a version that reads as "every finding is new" rather
// than as "the scheme moved".
const FingerprintVersion = "v1"

// fingerprintWidth is how much of the digest a marker carries — the same width
// the memory store uses for blob hashes.
const fingerprintWidth = 12

// Fingerprint is what identifies this finding across runs.
func (f Finding) Fingerprint() string {
	id := f.identity()
	sum := sha256.Sum256([]byte(id.path + "\x00" + id.category + "\x00" + id.evidence))
	return hex.EncodeToString(sum[:])[:fingerprintWidth]
}

// corroborate folds every instance's findings into one set of distinct claims,
// counting how many instances reached each.
//
// Agreement between independent instances is the confidence signal a quorum
// exists to produce, so the count is over instances rather than over findings:
// one instance that reports the same defect twice has not corroborated
// anything.
func corroborate(instances [][]Finding) []Finding {
	type group struct {
		finding   Finding
		instances map[int]bool
	}
	groups := map[identity]*group{}
	var order []identity

	for i, findings := range instances {
		for _, f := range findings {
			id := f.identity()
			g, seen := groups[id]
			if !seen {
				g = &group{finding: f, instances: map[int]bool{}}
				groups[id] = g
				order = append(order, id)
			}
			g.instances[i] = true
			// The most serious reading of a claim two instances disagree
			// about is the one that survives to the judge, which is the run
			// whose job is to decide it.
			if f.Severity.Rank() < g.finding.Severity.Rank() {
				g.finding.Severity = f.Severity
			}
		}
	}

	out := make([]Finding, 0, len(order))
	for _, id := range order {
		g := groups[id]
		f := g.finding
		f.Corroboration = len(g.instances)
		out = append(out, f)
	}
	return out
}

// sortFindings puts a set in the order a report reads it: most serious first,
// then by file and line, so two runs over the same change list the same
// findings in the same order.
func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Severity.Rank() != b.Severity.Rank() {
			return a.Severity.Rank() < b.Severity.Rank()
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return lineOf(a) < lineOf(b)
	})
}

// lineOf is a finding's first line, or zero when it has none — which sorts a
// whole-file claim above the line-anchored ones in the same file.
func lineOf(f Finding) int {
	if f.StartLine == nil {
		return 0
	}
	return *f.StartLine
}
