package prwait

import (
	"regexp"
	"strings"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

// LyditeMarker opens every lydite results comment.
const LyditeMarker = "<!-- lydite:results -->"

// DefaultLyditeApp is the slug of the GitHub App that writes lydite's results
// comment when Options.LyditeApp is empty.
const DefaultLyditeApp = "lydite"

// LyditeDetail is what lydite's results comment says, read as detail beside a
// verdict the head's checks decided.
type LyditeDetail struct {
	// Heading is the text of the comment's `### ` heading line.
	Heading string `json:"heading"`
	// Verdict is "green", "referral", "red" or "unknown", from the
	// heading's mark.
	Verdict string `json:"verdict"`
	// Rows are the ❌ and 🟡 rows, in the order the comment lists them.
	Rows      []LyditeRow `json:"rows"`
	UpdatedAt time.Time   `json:"updated_at"`
	// PossiblyStale reports that this wait saw lydite still running on the
	// head after the comment was last written, so the comment may describe
	// an earlier head.
	PossiblyStale bool `json:"possibly_stale"`
}

// LyditeRow is one row of a lydite section that failed or was referred.
type LyditeRow struct {
	// Section is the stage the row sits under: test, scan, referral, ….
	Section string `json:"section"`
	// Mark is "❌" or "🟡".
	Mark string `json:"mark"`
	// Text is the row's remaining cells, joined by " | ".
	Text string `json:"text"`
}

// lyditeDetail reads the latest lydite comment among comments, or returns
// nil when there is none. A comment is lydite's only when it opens with
// LyditeMarker and its author is exactly app's bot account, `<app>[bot]`,
// compared without case: any other App or GitHub Actions workflow can write the marker,
// and its rows would otherwise reach a session as findings to fix.
// lyditeRunningAt is the last time this wait saw a lydite check still
// running on the head, zero when it never did.
func lyditeDetail(comments []githubapp.IssueComment, app string, lyditeRunningAt time.Time) *LyditeDetail {
	var latest *githubapp.IssueComment
	for i := range comments {
		c := &comments[i]
		if !byLydite(c.Author, app) || !strings.HasPrefix(strings.TrimSpace(c.Body), LyditeMarker) {
			continue
		}
		if latest == nil || lastWritten(*c).After(lastWritten(*latest)) {
			latest = c
		}
	}
	if latest == nil {
		return nil
	}
	detail := ParseLydite(latest.Body)
	detail.UpdatedAt = lastWritten(*latest)
	detail.PossiblyStale = !lyditeRunningAt.IsZero() && detail.UpdatedAt.Before(lyditeRunningAt)
	return &detail
}

// byLydite reports whether login is app's bot account, `<app>[bot]`,
// compared without case as GitHub matches logins.
func byLydite(login, app string) bool {
	return strings.EqualFold(login, app+"[bot]")
}

// lyditeThread reports whether a review thread is lydite's alone: its
// opening comment and its latest are both by app's bot account. lydite opens
// such a thread on a line where a mutant survived and deletes it once the
// mutant is killed, and a surviving mutant fails lydite's own check, so that
// check carries the thread's verdict. A thread somebody else opened, or
// somebody else spoke last on, is judged like any other.
func lyditeThread(thread githubapp.ReviewThread, app string) bool {
	return byLydite(thread.RootAuthor, app) && byLydite(thread.LastAuthor, app)
}

// lastWritten is when a comment was last written: lydite edits one comment
// in place on every push, so its update time moves and its id does not.
func lastWritten(c githubapp.IssueComment) time.Time {
	if c.UpdatedAt.After(c.CreatedAt) {
		return c.UpdatedAt
	}
	return c.CreatedAt
}

// summarySectionRE finds the stage name a <details> section's <summary>
// carries in bold.
var summarySectionRE = regexp.MustCompile(`<b>([^<]*)</b>`)

// ParseLydite reads a lydite results comment's heading and its ❌ and 🟡
// rows.
//
// The format it is read against: the body opens with LyditeMarker; the first
// line beginning `### ` is the heading, whose mark is the verdict (✅ green,
// 🟡 referral, ❌ red); each stage is a <details> section whose <summary>
// names the stage in <b>…</b>; and each section holds a markdown table whose
// rows begin with a mark cell — | ✅ |, | 🟡 |, | ◽ | (informational) or
// | ❌ |. Anything else in the body is ignored, and a heading carrying none of
// the three marks is the verdict "unknown". The comment names the base it
// was measured from and never the head, which is why it is detail and never
// what decides a verdict.
func ParseLydite(body string) LyditeDetail {
	detail := LyditeDetail{Verdict: "unknown", Rows: []LyditeRow{}}
	section := ""
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case detail.Heading == "" && strings.HasPrefix(line, "### "):
			detail.Heading = strings.TrimSpace(strings.TrimPrefix(line, "### "))
			switch {
			case strings.Contains(detail.Heading, "❌"):
				detail.Verdict = "red"
			case strings.Contains(detail.Heading, "🟡"):
				detail.Verdict = "referral"
			case strings.Contains(detail.Heading, "✅"):
				detail.Verdict = "green"
			}
		case strings.Contains(line, "<summary>"):
			if m := summarySectionRE.FindStringSubmatch(line); m != nil {
				section = strings.TrimSpace(m[1])
			}
		case strings.HasPrefix(line, "|"):
			cells := strings.Split(strings.Trim(line, "|"), "|")
			mark := strings.TrimSpace(cells[0])
			if mark != "❌" && mark != "🟡" {
				continue
			}
			rest := make([]string, 0, len(cells)-1) // [lydite:exclude_from_mutation][the size is only a capacity hint: a slice grows past it, so a larger hint yields the same rows]
			for _, cell := range cells[1:] {
				rest = append(rest, strings.TrimSpace(cell))
			}
			detail.Rows = append(detail.Rows, LyditeRow{Section: section, Mark: mark, Text: strings.Join(rest, " | ")})
		}
	}
	return detail
}
