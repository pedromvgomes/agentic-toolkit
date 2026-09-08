// Package reviewapprove decides whether a reviewed head may be approved, and
// posts the approval when it may.
//
// It owns the approval event and the call that sends it, and nothing else in
// the binary names either. No code path from a review run reaches this package
// — a run that could approve the code it just reviewed is the hazard GitHub
// blocks GITHUB_TOKEN approvals to prevent, and ADR 0006 makes it a property
// of the import graph rather than of a prompt. A person types the command.
package reviewapprove

import (
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

// Marking is what a reply says to clear a finding.
//
// A written statement rather than a click. Resolving a thread says the
// conversation is finished, which is a different claim from "this is not a
// defect", and a defect cleared by a click is the checklist this design
// removed --force to avoid.
const Marking = "agtk: false positive"

// FalsePositive is one finding somebody with write access has said is not a
// defect.
type FalsePositive struct {
	// Reason is what they wrote. Required: a marking with no reason is the
	// click this asks for a sentence instead of.
	Reason string
}

// markFalsePositive reads a reply as a marking, or reports that it is not one.
//
// Written and read here, in the package that acts on it, for the reason the
// markers are written and read in one file: the syntax a refusal tells somebody
// to type and the syntax approval accepts are one format, and two definitions
// of it drift into a gate nobody can pass.
//
// The marking opens a line, so a reply quoting the syntax while arguing
// against it does not clear anything. What follows on that line is the reason,
// and where the line carries none the rest of the reply is.
func markFalsePositive(body string) (FalsePositive, bool) {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < len(Marking) || !strings.EqualFold(trimmed[:len(Marking)], Marking) {
			continue
		}
		reason := strings.TrimSpace(strings.TrimLeft(trimmed[len(Marking):], " \t:.,-–—"))
		if reason == "" {
			reason = strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		}
		if reason == "" {
			return FalsePositive{}, false
		}
		return FalsePositive{Reason: reason}, true
	}
	return FalsePositive{}, false
}

// clearedBy reports whether any reply on a thread marks its finding a false
// positive from an account that can push.
//
// Write access rather than anyone who can comment, because the author of a
// change is the party a review does not trust: a finding its own author could
// dismiss is one an injected instruction can dismiss too, which is the
// conversion ADR 0007 exists to prevent reached at the last step instead of
// the first.
func clearedBy(thread githubapp.AnsweredThread) (FalsePositive, bool) {
	for _, reply := range thread.Replies {
		if !reply.CanWrite() {
			continue
		}
		if marked, ok := markFalsePositive(reply.Body); ok {
			return marked, true
		}
	}
	return FalsePositive{}, false
}
