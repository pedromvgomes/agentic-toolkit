// Package reviewpost turns a finished review into the one API call that posts
// it, and makes that call.
//
// Separate from internal/reviewrun because producing a review and transmitting
// it are separate acts: a panel costs real money, and a transport failure must
// be retryable without re-running it. --dry-run costing nothing to build is
// the same separation read from the other side — ADR 0006.
package reviewpost

import (
	"fmt"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/reviewrun"
)

// FingerprintMarkerPrefix opens the HTML comment a posted inline comment
// carries its fingerprint in.
//
// Named here and defined beside the reader of it. Writing the fingerprint
// marker and reading it back are one format, and a second definition of where
// the version sits would drift silently: a fingerprint marker that does not
// parse suppresses nothing, which looks exactly like a pull request carrying
// nothing to suppress.
const FingerprintMarkerPrefix = reviewrun.FingerprintMarkerPrefix

// FingerprintMarker renders the fingerprint marker for one fingerprint.
func FingerprintMarker(fingerprint string) string {
	return reviewrun.FingerprintMarker(fingerprint)
}

// Placement is what became of each surviving finding when the review was laid
// out for GitHub.
//
// The grouping is by what a reader can do about a finding, not by why it is
// not inline. A finding blocks approval only where agtk can give somebody a
// thread to answer on, so the one distinction that matters downstream is
// whether the finding got a comment at all.
type Placement struct {
	// Inline are the findings that became inline comments, in the order they
	// were posted. They ride inside the review's one request.
	Inline []reviewrun.Finding
	// FileLevel are the findings that hang off a whole file: one that carries
	// no line, and one whose line the diff does not add. GitHub will not
	// accept either as an inline comment and will accept both against the
	// path, so each becomes a comment of its own — a separate request after
	// the review, because subject_type is not a field a review's draft
	// comments carry.
	FileLevel []reviewrun.Finding
	// Unattachable are the findings agtk can give nobody a thread to answer
	// on: one naming no path, and one naming a path the change does not
	// touch, which GitHub refuses. They are stated in the review body and
	// gate nothing — a gate with no remedy is a deadlock rather than a
	// control. A prompt-injection finding here is the one exception, and the
	// deadlock is the point.
	Unattachable []reviewrun.Finding
}

// Total is how many findings were laid out.
func (p Placement) Total() int {
	return len(p.Inline) + len(p.FileLevel) + len(p.Unattachable)
}

// Deadlocked lists the prompt-injection findings agtk could not attach.
//
// The material under review addresses the reviewer, agtk can offer nobody a
// thread to answer on, and the remedy is to change the code. ADR 0007.
func (p Placement) Deadlocked() []reviewrun.Finding {
	var out []reviewrun.Finding
	for _, f := range p.Unattachable {
		if f.Injected() {
			out = append(out, f)
		}
	}
	return out
}

// AddedLines is which lines of each file a change adds, keyed by path.
type AddedLines map[string]map[int]bool

// holds reports whether a comment on path at line would land on the diff.
func (a AddedLines) holds(path string, line int) bool {
	lines, ok := a[path]
	return ok && lines[line]
}

// touches reports whether the change names this path at all, which is what
// decides whether GitHub will accept a comment against the whole file.
//
// An empty path is not a path: a finding that names no file has nowhere to
// hang even when the diff is enormous.
func (a AddedLines) touches(path string) bool {
	if path == "" {
		return false
	}
	_, ok := a[path]
	return ok
}

// Build lays a finished review out as the payload that posts it, and the
// file-level comments that follow it.
//
// Every finding reaches the pull request. Where it reaches — an inline
// comment, a comment against the file, or the review body — is decided here,
// by whether GitHub will accept a comment where the finding points.
func Build(r *reviewrun.Review, pr githubapp.PullRequest, added AddedLines) (githubapp.ReviewPayload, Placement) {
	var place Placement
	comments := []githubapp.ReviewComment{}

	for _, f := range r.Findings {
		switch {
		case !added.touches(f.Path):
			place.Unattachable = append(place.Unattachable, f)
		case !f.HasLine() || !positionable(f, added):
			place.FileLevel = append(place.FileLevel, f)
		default:
			comments = append(comments, comment(f, added))
			place.Inline = append(place.Inline, f)
		}
	}

	return githubapp.ReviewPayload{
		CommitID: pr.HeadSHA,
		Body:     Body(r, pr, place),
		Event:    githubapp.EventComment,
		Comments: comments,
	}, place
}

// FileComments renders the comments that are posted against a whole file,
// after the review itself.
func FileComments(pr githubapp.PullRequest, place Placement) []githubapp.FileComment {
	out := make([]githubapp.FileComment, 0, len(place.FileLevel))
	for _, f := range place.FileLevel {
		out = append(out, githubapp.FileComment{
			CommitID: pr.HeadSHA,
			Path:     f.Path,
			Body:     CommentBody(f),
		})
	}
	return out
}

// positionable reports whether a finding names a line a comment can land on.
//
// The end of the region rather than its start, because that is the line an
// inline comment is anchored at and therefore the one GitHub validates. A
// finding whose region ends outside the diff is refused even when it begins
// inside it.
func positionable(f reviewrun.Finding, added AddedLines) bool {
	return added.holds(f.Path, anchorLine(f))
}

// AnchorLine is the line an inline comment is attached to, and therefore the
// line GitHub validates against the diff.
//
// Exported because a run that could not position a finding has to say which
// line it could not position it on, and that is this one rather than the
// finding's start: a region beginning on the diff and ending off it is refused
// for its end.
func AnchorLine(f reviewrun.Finding) int { return anchorLine(f) }

// anchorLine is the line an inline comment is attached to.
func anchorLine(f reviewrun.Finding) int {
	if f.EndLine != nil && *f.EndLine >= *f.StartLine {
		return *f.EndLine
	}
	return *f.StartLine
}

// comment renders one finding as an inline comment.
//
// A multi-line comment is asked for only when every line between the two ends
// is also on the diff. GitHub validates the whole span, so a region reaching
// back across unchanged code is refused — and one refusal costs the batch.
func comment(f reviewrun.Finding, added AddedLines) githubapp.ReviewComment {
	line := anchorLine(f)
	out := githubapp.ReviewComment{
		Path: f.Path,
		Line: line,
		Side: githubapp.SideRight,
		Body: CommentBody(f),
	}
	if start := *f.StartLine; start < line && spanIsAdded(f.Path, start, line, added) {
		side := githubapp.SideRight
		out.StartLine = &start
		out.StartSide = &side
	}
	return out
}

// spanIsAdded reports whether every line from start to end is on the diff.
func spanIsAdded(path string, start, end int, added AddedLines) bool {
	for line := start; line <= end; line++ {
		if !added.holds(path, line) {
			return false
		}
	}
	return true
}

// CommentBody renders one finding as the markdown of an inline comment.
func CommentBody(f reviewrun.Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s — %s**\n\n%s\n", f.Severity, f.Category, prose(f.Issue))
	if s := prose(f.Suggestion); s != "" {
		fmt.Fprintf(&b, "\n%s\n", s)
	}
	fmt.Fprintf(&b, "\n%s\n", attribution(f))
	// Last, and on its own line: a marker inside a paragraph is still
	// invisible, but a reader diffing raw bodies should find it in one place.
	fmt.Fprintf(&b, "\n%s\n", FingerprintMarker(f.Fingerprint()))
	return b.String()
}

// prose renders a finding's own words, with nothing in them able to open an
// HTML comment.
//
// A body carries exactly one fingerprint marker, and a later run reads
// identity back off the pull request from it. The words around it are written
// by a model that read a diff somebody else wrote, so text arriving as an
// issue or a suggestion can carry a marker of its own choosing — and a reader
// that found two would have no way to tell which one this review meant.
// Breaking the opening delimiter is enough: what is left renders as the four
// characters a person sees, and matches nothing.
func prose(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "<!--", "&lt;!--")
}

// attribution says who reported a finding and what happened to it on the way
// here, so a reader can weigh a claim one reviewer made alone against one
// three reached independently.
func attribution(f reviewrun.Finding) string {
	parts := []string{"reported by " + f.Reviewer}
	if f.Corroboration > 1 {
		parts = append(parts, fmt.Sprintf("%d instances agreed", f.Corroboration))
	}
	if f.Verdict != nil && f.Verdict.Verdict != "" {
		parts = append(parts, "validator "+f.Verdict.Verdict)
	}
	return "<sub>" + strings.Join(parts, " · ") + "</sub>"
}
