package reviewrun

import (
	"fmt"
	"strings"
)

// Report is what one run produced, or why it produced nothing.
//
// The findings are unexported and reachable only through Findings, which
// returns availability alongside them. A caller cannot read an empty slice as
// "this run found nothing" without also being handed the answer to "did this
// run answer at all" — which is the same shape review.Count makes for a
// number that is unknown rather than low, and for the same reason: a review
// reporting nothing looks exactly like a clean review, and a clean review is
// what unblocks approval.
type Report struct {
	// Available reports whether the run answered.
	Available bool
	// Reason says why it did not, and is empty when it did.
	Reason string
	// Blocked reports that the run's provider declined to serve the
	// credential rather than attempting and failing — the one outcome a
	// caller may route around by trying a different provider. False for
	// every other reason a run did not answer.
	Blocked bool

	findings []Finding
}

// Answered builds a Report for a run that produced findings.
func Answered(findings []Finding) Report {
	return Report{Available: true, findings: findings}
}

// Unavailable builds a Report for a run that could not answer.
func Unavailable(format string, args ...interface{}) Report {
	return Report{Reason: fmt.Sprintf(format, args...)}
}

// Blocked builds a Report for a run whose provider declined to serve the
// credential — a quota exhausted or a credential rejected — rather than
// attempting the run and failing at it.
func Blocked(format string, args ...interface{}) Report {
	return Report{Reason: fmt.Sprintf(format, args...), Blocked: true}
}

// Findings returns what the run reported and whether it reported at all.
//
// The bool is not ignorable at a call site that wants the slice, which is the
// whole point: `len(r.Findings()) == 0` on an outage would read as a clean
// review.
func (r Report) Findings() ([]Finding, bool) {
	if !r.Available {
		return nil, false
	}
	return r.findings, true
}

// Count is how many findings the run reported, and is meaningful only for a
// run that answered.
func (r Report) Count() int {
	if !r.Available {
		return 0
	}
	return len(r.findings)
}

// String renders the report's availability, for a status line.
func (r Report) String() string {
	if r.Available {
		return fmt.Sprintf("%d findings", len(r.findings))
	}
	return "could not answer: " + r.Reason
}

// Review is everything one `agtk code-review run` produced.
type Review struct {
	// Panel is the panel that ran.
	Panel string
	// Manifest names which manifest was read.
	Manifest string
	// Range is what the change was measured over.
	Range string

	// Findings are what survived the judge, in report order.
	Findings []Finding
	// Good is what the judge thought was done well.
	Good []string

	// Reports is every run that was made, keyed by the label it was made
	// under, in the order they were scheduled.
	Reports []RunReport
	// Skipped lists what the review root does not hold.
	Skipped []Skipped
	// Conventions names the convention documents that were read.
	Conventions []string
	// MissingConventions names documents the manifest asked for that the base
	// ref does not hold. A repo that believes it is being held to its own
	// rules and is not needs to be told.
	MissingConventions []string
	// DiscardedIDs are ids the judge returned that agtk did not issue.
	DiscardedIDs []string
	// ReattachedIDs are prompt-injection findings the judge dropped and agtk
	// put back. The judge narrows freely everywhere else; this is the one
	// category it may not decide, so a run that exercised the carve-out says
	// so rather than presenting the set as the judge's own.
	ReattachedIDs []string
	// DroppedByValidator counts candidate findings a validator rejected.
	DroppedByValidator int
	// Threads is what the pull request already carried, or why that could not
	// be read. A local review reads none, and says so the same way.
	Threads Threads
	// Suppressed are the findings an existing thread already carries, which
	// are therefore not posted. Kept rather than counted, because a run that
	// posts nothing has to be able to say what it withheld and why.
	Suppressed []Suppression
	// CostUSD is what the whole review spent, and is zero when no provider
	// reported a cost.
	CostUSD float64

	// Available reports whether the review reached a verdict at all. A
	// failing judge makes it false; a failing reviewer only makes it partial.
	Available bool
	// Reason says why it did not, and is empty when it did.
	Reason string
	// Blocked reports that the review stayed unavailable because every
	// provider it could try declined to serve the credential — the one
	// unavailable reason a caller posts nothing for, rather than surfacing
	// visibly like an ordinary failure.
	Blocked bool
	// FallbackFrom names the panel this review's own Panel was tried in
	// place of, after every run on it was blocked. Empty when no fallback
	// was attempted.
	FallbackFrom string

	// injectedCarry holds this run's own prompt-injection candidates, kept
	// once validation clears them whether or not the judge went on to
	// answer. A fallback that reaches a verdict on a different panel splices
	// these into its own Findings by Fingerprint, so an injection the first,
	// blocked panel's reviewers already caught cannot be silently absent
	// from a review that ends up looking clean — the conversion ADR 0007
	// and ADR 0008 exist to prevent, reopened at the one seam this feature
	// adds between the reviewer that catches an injection and the judge
	// that would otherwise re-attach it.
	injectedCarry []Finding
}

// RunReport is one Runner's outcome, for the review record.
type RunReport struct {
	// Label names the run: the reviewer's manifest name, plus an instance
	// number when the panel's quorum is above one.
	Label string
	// Role is what the run was: reviewer, validator or judge.
	Role string
	// Provider and Model are what actually ran.
	Provider string
	Model    string
	// Report is what it produced, or why it produced nothing.
	Report Report
	// CostUSD is what the run spent.
	CostUSD float64
}

// Roles a run is made in.
const (
	RoleReviewer  = "reviewer"
	RoleValidator = "validator"
	RoleJudge     = "judge"
)

// Partial reports whether any run could not answer. A review that reached a
// verdict on three reviewers out of four is still a verdict, and the person
// reading it has to be told which quarter is missing.
func (r *Review) Partial() bool {
	for _, run := range r.Reports {
		if !run.Report.Available {
			return true
		}
	}
	return false
}

// Unanswered lists the runs that could not answer.
func (r *Review) Unanswered() []RunReport {
	var out []RunReport
	for _, run := range r.Reports {
		if !run.Report.Available {
			out = append(out, run)
		}
	}
	return out
}

// Silent lists the reviewers that answered and had no opinion.
//
// They get their own line in the report rather than being absent from it: a
// reviewer that ran and found nothing and a reviewer that never ran are the
// same shape in a table that only lists findings, and they mean opposite
// things.
func (r *Review) Silent() []RunReport {
	var out []RunReport
	for _, run := range r.Reports {
		if run.Role == RoleReviewer && run.Report.Available && run.Report.Count() == 0 {
			out = append(out, run)
		}
	}
	return out
}

// Counts returns how many surviving findings carry each severity.
func (r *Review) Counts() map[Severity]int {
	counts := map[Severity]int{}
	for _, f := range r.Findings {
		counts[f.Severity]++
	}
	return counts
}

// runsMade is how many runs the review made.
func (r *Review) runsMade() int { return len(r.Reports) }

// Record renders the review record: what ran, what did not answer, and what it
// cost.
func (r *Review) Record() string {
	var b strings.Builder
	fmt.Fprintf(&b, "panel %s, %d runs", r.Panel, r.runsMade())
	if n := len(r.Unanswered()); n > 0 {
		fmt.Fprintf(&b, ", %d could not answer", n)
	}
	if r.DroppedByValidator > 0 {
		fmt.Fprintf(&b, ", %d dropped by a validator", r.DroppedByValidator)
	}
	if n := len(r.Suppressed); n > 0 {
		fmt.Fprintf(&b, ", %d already on the pull request", n)
	}
	if n := len(r.Conventions); n > 0 {
		fmt.Fprintf(&b, ", %d convention docs read", n)
	}
	if r.CostUSD > 0 {
		fmt.Fprintf(&b, ", $%.4f", r.CostUSD)
	}
	return b.String()
}
