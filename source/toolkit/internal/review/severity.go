package review

// Severity is how much a finding matters.
//
// The ladder lives here rather than beside the findings that carry one because
// the manifest names a rung: the approval floor is a repo's own setting, and a
// manifest cannot be validated against a vocabulary it cannot see.
type Severity string

const (
	// SeverityRed means a defect serious enough that merging with it is the
	// wrong call.
	SeverityRed Severity = "RED"
	// SeverityAmber means a defect that is real and smaller. It obliges what
	// RED obliges — a fix, or a written statement that it is not a defect —
	// and is the default approval floor.
	SeverityAmber Severity = "AMBER"
	// SeverityGreen is a remark rather than a defect. It obliges only that its
	// thread be resolved.
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

// AtOrAbove reports whether s is at least as serious as floor.
//
// The comparison approval is decided by, in one place: Rank is inverted — the
// most serious rung is zero — and a call site that wrote the inequality by
// hand would get it the wrong way round exactly once, and the failure is an
// approval granted over a defect.
func (s Severity) AtOrAbove(floor Severity) bool { return s.Rank() <= floor.Rank() }

// SeverityNames renders the ladder for a diagnostic that has to say which
// words are accepted.
func SeverityNames() []string {
	out := make([]string, 0, len(Severities))
	for _, s := range Severities {
		out = append(out, string(s))
	}
	return out
}
