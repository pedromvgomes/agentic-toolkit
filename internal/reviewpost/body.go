package reviewpost

import (
	"fmt"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun"
)

// Body renders the review's summary: what ran, what it found, and everything
// that could not be said inline.
//
// A review is a complete statement about one commit. Nothing accumulates
// inside it and an older review is history rather than stale current state,
// so the body restates the whole picture each time rather than referring to
// what an earlier one said.
func Body(r *reviewrun.Review, pr githubapp.PullRequest, place Placement) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## Review by `agtk` — panel `%s`\n\n", r.Panel)

	if !r.Available {
		fmt.Fprintf(&b, "**This review did not reach a verdict:** %s\n\n", r.Reason)
		writeRuns(&b, r)
		writeRecord(&b, r, pr, place)
		return b.String()
	}

	writeSummaryLine(&b, r, place)
	writeFindingList(&b, "Findings with nowhere to answer", place.Unattachable,
		"Each names a path this pull request does not change, or no path at all. GitHub refuses a "+
			"comment there, so there is no thread to answer on and these block nothing — a gate with "+
			"no remedy is a deadlock rather than a control.")
	writeDeadlock(&b, place)

	if len(r.Good) > 0 {
		b.WriteString("### What's good\n\n")
		for _, g := range r.Good {
			fmt.Fprintf(&b, "- %s\n", g)
		}
		b.WriteString("\n")
	}

	writeRuns(&b, r)
	writeRecord(&b, r, pr, place)
	return b.String()
}

// writeDeadlock says that a prompt-injection finding could not be attached,
// and that approval is therefore closed until the code changes.
//
// It is the one finding a reader cannot answer their way past. The material
// under review addresses the reviewer, agtk can offer nobody a thread, and the
// deadlock is the point — ADR 0007.
func writeDeadlock(b *strings.Builder, place Placement) {
	deadlocked := place.Deadlocked()
	if len(deadlocked) == 0 {
		return
	}
	fmt.Fprintf(b, "**This pull request cannot be approved until the code changes.** %d of the findings "+
		"above quote text in the reviewed material addressed at the reviewer, and name a path no comment "+
		"can hang off. There is nothing to reply to and no flag that overrides it: the remedy is to remove "+
		"the text.\n\n", len(deadlocked))
}

// reviewMarker renders what approval later reads back out of this body.
//
// Nothing is persisted between runs, so the pull request is the only record of
// what a review found. A finding agtk could give nobody a thread to answer on
// is recorded as such, because approval must not demand an answer that cannot
// be written.
func reviewMarker(r *reviewrun.Review, pr githubapp.PullRequest, place Placement) string {
	marker := reviewrun.ReviewMarker{
		Head: pr.HeadSHA,
		// A verdict is the judge answering, every reviewer answering, and the
		// pull request's threads being readable. A run missing any of those
		// found less than it would have, and "found nothing" is the one thing
		// approval must never read that as.
		Complete: r.Available && !r.Partial() && r.Threads.Available,
	}
	for _, group := range []struct {
		findings   []reviewrun.Finding
		answerable bool
	}{
		{place.Inline, true},
		{place.FileLevel, true},
		{place.Unattachable, false},
	} {
		for _, f := range group.findings {
			marker.Findings = append(marker.Findings, reviewrun.MarkedFinding{
				Fingerprint: f.Fingerprint(),
				Severity:    f.Severity,
				Answerable:  group.answerable,
				Injected:    f.Injected(),
			})
		}
	}
	return marker.Render()
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
	fmt.Fprintf(b, "%s. %d posted as inline comments, %d against a whole file.\n\n",
		strings.Join(parts, ", "), len(place.Inline), len(place.FileLevel))
}

// writeFindingList renders the findings that could not be inline comments.
func writeFindingList(b *strings.Builder, heading string, findings []reviewrun.Finding, why string) {
	if len(findings) == 0 {
		return
	}
	fmt.Fprintf(b, "### %s\n\n%s\n\n", heading, why)
	for _, f := range findings {
		fmt.Fprintf(b, "**%s — %s** · `%s`\n\n%s\n",
			f.Severity, f.Category, location(f), prose(f.Issue))
		if s := prose(f.Suggestion); s != "" {
			fmt.Fprintf(b, "\n%s\n", s)
		}
		if e := strings.TrimSpace(f.Evidence); e != "" {
			fmt.Fprintf(b, "\n%s\n%s\n%s\n", fence(e), e, fence(e))
		}
		fmt.Fprintf(b, "\n%s\n\n", attribution(f))
	}
}

// fence is a code fence long enough to hold body.
//
// Evidence is quoted code carried byte for byte from the reviewer that
// produced it, and ADR 0008 is why it may not be edited on the way here. A
// fixed three-backtick fence therefore ends wherever the quoted code happens
// to contain three backticks — which quoted Markdown routinely does — and
// everything after that renders as the review's own prose. Widening the fence
// past the longest run inside it closes that without touching a byte of the
// quote.
func fence(body string) string {
	longest, run := 0, 0
	for _, r := range body {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
			continue
		}
		run = 0
	}
	return strings.Repeat("`", max(3, longest+1))
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
	writeThreads(b, r)
}

// writeThreads states what this pull request already carried and what that
// withheld.
//
// In the body for the reason every other gap is: a review that suppressed
// nothing and a review whose thread list never arrived post the same comments,
// and a reader has to be able to tell "there was nothing already said" from
// "this run could not look".
func writeThreads(b *strings.Builder, r *reviewrun.Review) {
	if !r.Threads.Available {
		// A review of a working tree reads no threads and has none to report.
		// Only an attempted read carries a reason.
		if r.Threads.Reason == "" {
			return
		}
		fmt.Fprintf(b, "**The existing comment threads could not be read:** %s\n\n"+
			"Nothing was withheld on the strength of what this pull request already carries, so this "+
			"review may repeat a finding that is already on it.\n\n", r.Threads.Reason)
		return
	}
	if read := r.Threads.Count(); read > 0 && len(r.Threads.Identified()) == 0 {
		fmt.Fprintf(b, "%d existing comment thread(s) were read and none carries a fingerprint this run can match, "+
			"so nothing could be withheld on identity. A finding this review already made is stated again.\n\n", read)
	}
	if n := len(r.Suppressed); n > 0 {
		fmt.Fprintf(b, "### Already on this pull request (%d)\n\n"+
			"Not posted again. A finding is withheld only when the code it quotes is byte-identical to "+
			"one an existing thread quotes, so a fix that changed the code is a new finding rather than "+
			"a suppressed one.\n\n", n)
		for _, s := range r.Suppressed {
			fmt.Fprintf(b, "- `%s` — %s · %s\n", location(s.Finding), s.Finding.Category, s.Reason)
		}
		b.WriteString("\n")
	}
	if other := r.Threads.AtOtherVersion(); len(other) > 0 {
		fmt.Fprintf(b, "%d existing thread(s) carry a fingerprint from another scheme version. "+
			"The hash behind one was taken over different bytes, so it identifies nothing here and a "+
			"finding matching it is posted again.\n\n", len(other))
	}
}

// writeRecord closes with what ran, what it cost, and the marker approval
// reads this review back out of.
//
// The marker is last and on its own line: it is invisible in rendered markdown
// either way, and a reader diffing raw bodies should find it in one place.
func writeRecord(b *strings.Builder, r *reviewrun.Review, pr githubapp.PullRequest, place Placement) {
	fmt.Fprintf(b, "<sub>%s · range `%s` · manifest `%s`</sub>\n", r.Record(), r.Range, r.Manifest)
	fmt.Fprintf(b, "\n%s\n", reviewMarker(r, pr, place))
}
