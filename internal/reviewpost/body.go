package reviewpost

import (
	"fmt"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun"
)

// Body renders the review's summary: what ran, what it found, and everything
// that could not be said inline.
//
// A review is a complete statement about one commit. Nothing accumulates
// inside it and an older review is history rather than stale current state,
// so the body restates the whole picture each time rather than referring to
// what an earlier one said.
func Body(r *reviewrun.Review, place Placement) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## Review by `agtk` — panel `%s`\n\n", r.Panel)

	if !r.Available {
		fmt.Fprintf(&b, "**This review did not reach a verdict:** %s\n\n", r.Reason)
		writeRuns(&b, r)
		writeRecord(&b, r)
		return b.String()
	}

	writeSummaryLine(&b, r, place)
	writeFindingList(&b, "Findings with no line", place.CrossCutting,
		"A claim about a subsystem rather than about a statement. GitHub requires a path and a line for an inline comment, so it is stated here.")
	writeFindingList(&b, "Findings outside this diff", place.Unpositioned,
		"These name code the pull request does not change. GitHub refuses an inline comment there, and one refused comment discards every comment in the review, so they are stated here instead.")

	if len(r.Good) > 0 {
		b.WriteString("### What's good\n\n")
		for _, g := range r.Good {
			fmt.Fprintf(&b, "- %s\n", g)
		}
		b.WriteString("\n")
	}

	writeRuns(&b, r)
	writeRecord(&b, r)
	return b.String()
}

// writeSummaryLine states the count at each severity, and says plainly when
// there is none.
func writeSummaryLine(b *strings.Builder, r *reviewrun.Review, place Placement) {
	if place.Total() == 0 {
		b.WriteString("No findings survived the panel.\n\n")
		return
	}
	counts := r.Counts()
	var parts []string
	for _, sev := range reviewrun.Severities {
		if n := counts[sev]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, sev))
		}
	}
	fmt.Fprintf(b, "%s. %d posted as inline comments.\n\n", strings.Join(parts, ", "), len(place.Inline))
}

// writeFindingList renders the findings that could not be inline comments.
func writeFindingList(b *strings.Builder, heading string, findings []reviewrun.Finding, why string) {
	if len(findings) == 0 {
		return
	}
	fmt.Fprintf(b, "### %s\n\n%s\n\n", heading, why)
	for _, f := range findings {
		fmt.Fprintf(b, "**%s — %s** · `%s`\n\n%s\n",
			f.Severity, f.Category, location(f), strings.TrimSpace(f.Issue))
		if s := strings.TrimSpace(f.Suggestion); s != "" {
			fmt.Fprintf(b, "\n%s\n", s)
		}
		if e := strings.TrimSpace(f.Evidence); e != "" {
			fmt.Fprintf(b, "\n```\n%s\n```\n", e)
		}
		fmt.Fprintf(b, "\n%s\n\n", attribution(f))
	}
}

// location renders where a finding is, for a body entry that has no inline
// anchor to say it.
func location(f reviewrun.Finding) string {
	if f.Path == "" {
		return "(no file)"
	}
	if !f.HasLine() {
		return f.Path
	}
	if f.EndLine != nil && *f.EndLine > *f.StartLine {
		return fmt.Sprintf("%s:%d-%d", f.Path, *f.StartLine, *f.EndLine)
	}
	return fmt.Sprintf("%s:%d", f.Path, *f.StartLine)
}

// writeRuns reports the gaps: a reviewer that could not answer, and one that
// answered and had no opinion.
//
// Both go in the body because a review reporting nothing looks exactly like a
// clean review, and a clean review is what unblocks approval. A reader has to
// be able to tell "nobody found anything" from "a quarter of the panel never
// ran".
func writeRuns(b *strings.Builder, r *reviewrun.Review) {
	if unanswered := r.Unanswered(); len(unanswered) > 0 {
		fmt.Fprintf(b, "### Could not answer (%d)\n\n", len(unanswered))
		for _, run := range unanswered {
			fmt.Fprintf(b, "- `%s`: %s\n", run.Label, run.Report.Reason)
		}
		b.WriteString("\nThis review is partial: what these would have found is unknown, not absent.\n\n")
	}
	if silent := r.Silent(); len(silent) > 0 {
		names := make([]string, 0, len(silent))
		for _, run := range silent {
			names = append(names, "`"+run.Label+"`")
		}
		fmt.Fprintf(b, "Ran and reported nothing: %s\n\n", strings.Join(names, ", "))
	}
	if len(r.MissingConventions) > 0 {
		fmt.Fprintf(b, "Convention documents the manifest names that the base ref does not hold: %s\n\n",
			strings.Join(r.MissingConventions, ", "))
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(b, "Absent from the reviewed copy (%d): a symlink, a submodule or a file too large to read is not code this review looked at.\n\n",
			len(r.Skipped))
	}
}

// writeRecord closes with what ran and what it cost.
func writeRecord(b *strings.Builder, r *reviewrun.Review) {
	fmt.Fprintf(b, "<sub>%s · range `%s` · manifest `%s`</sub>\n", r.Record(), r.Range, r.Manifest)
}
