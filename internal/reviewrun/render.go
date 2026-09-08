package reviewrun

import (
	"fmt"
	"io"
	"strings"
)

// Render writes a review as the triage tables a person reads.
//
// Numbered continuously across severities, so somebody can say "fix 1, 4 and
// 7" without saying which table they meant.
func Render(w io.Writer, r *Review) {
	fmt.Fprintf(w, "manifest: %s\n", r.Manifest)
	fmt.Fprintf(w, "range:    %s\n", r.Range)
	fmt.Fprintf(w, "panel:    %s\n\n", r.Panel)

	if !r.Available {
		fmt.Fprintf(w, "This review could not reach a verdict: %s\n\n", r.Reason)
		renderRuns(w, r)
		return
	}

	n := 0
	for _, sev := range Severities {
		group := findingsAt(r.Findings, sev)
		if len(group) == 0 {
			continue
		}
		fmt.Fprintf(w, "%s — %s\n", sev, severityGloss(sev))
		for _, f := range group {
			n++
			renderFinding(w, n, f)
		}
		fmt.Fprintln(w)
	}

	if len(r.Findings) == 0 {
		fmt.Fprintln(w, "No findings survived the panel.")
		fmt.Fprintln(w)
	}

	if len(r.Good) > 0 {
		fmt.Fprintln(w, "What's good")
		for _, g := range r.Good {
			fmt.Fprintf(w, "  - %s\n", g)
		}
		fmt.Fprintln(w)
	}

	renderRuns(w, r)
	fmt.Fprintf(w, "Review record: %s\n", r.Record())
}

// renderFinding writes one numbered row.
func renderFinding(w io.Writer, n int, f Finding) {
	fmt.Fprintf(w, "  %2d. %s  %s\n", n, location(f), f.Category)
	fmt.Fprintf(w, "      %s\n", f.Issue)
	if f.Suggestion != "" {
		fmt.Fprintf(w, "      fix: %s\n", f.Suggestion)
	}
	detail := fmt.Sprintf("      — %s", f.Reviewer)
	if f.Corroboration > 1 {
		detail += fmt.Sprintf(", %d instances agreed", f.Corroboration)
	}
	if f.Verdict != nil {
		detail += ", validator " + f.Verdict.Verdict
	}
	fmt.Fprintln(w, detail)
}

// renderRuns reports what ran, what did not answer, and who had no opinion.
//
// A reviewer that ran and found nothing gets a line. Absent from the tables it
// looks identical to a reviewer that never ran, and the two mean opposite
// things: one is evidence, the other is a gap.
func renderRuns(w io.Writer, r *Review) {
	if unanswered := r.Unanswered(); len(unanswered) > 0 {
		fmt.Fprintf(w, "Could not answer (%d):\n", len(unanswered))
		for _, run := range unanswered {
			fmt.Fprintf(w, "  - %s: %s\n", run.Label, run.Report.Reason)
		}
		fmt.Fprintln(w)
	}
	if silent := r.Silent(); len(silent) > 0 {
		names := make([]string, 0, len(silent))
		for _, run := range silent {
			names = append(names, run.Label)
		}
		fmt.Fprintf(w, "Ran and reported nothing: %s\n\n", strings.Join(names, ", "))
	}
	if len(r.ReattachedIDs) > 0 {
		fmt.Fprintf(w, "The judge dropped %d prompt-injection finding(s); they were put back, because that category is not the judge's to drop: %s\n\n",
			len(r.ReattachedIDs), strings.Join(r.ReattachedIDs, ", "))
	}
	if len(r.DiscardedIDs) > 0 {
		fmt.Fprintf(w, "The judge returned %d id(s) this review did not issue, and they were discarded: %s\n\n",
			len(r.DiscardedIDs), strings.Join(r.DiscardedIDs, ", "))
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(w, "Absent from the reviewed copy (%d):\n", len(r.Skipped))
		for _, line := range summariseSkipped(r.Skipped) {
			fmt.Fprintf(w, "  - %s\n", line)
		}
		fmt.Fprintln(w)
	}
	renderThreads(w, r)
}

// renderThreads says what the pull request already carried and what that
// withheld.
//
// A review that found nothing already said and a review whose thread list
// never arrived print the same tables, and they mean opposite things: the
// first checked, the second does not know. A thread read that failed is
// therefore stated, because "nothing was suppressed" is the output of both.
func renderThreads(w io.Writer, r *Review) {
	if !r.Threads.Available {
		// A review of a working tree reads no threads and has none to report:
		// there is no pull request holding any. Only a read that was attempted
		// and failed carries a reason.
		if r.Threads.Reason == "" {
			return
		}
		fmt.Fprintf(w, "Could not read the existing threads on this pull request: %s\n", r.Threads.Reason)
		fmt.Fprintf(w, "Nothing was withheld, so this review may repeat what the pull request already carries.\n\n")
		return
	}
	if read := r.Threads.Count(); read > 0 {
		identified := len(r.Threads.Identified())
		if identified == 0 {
			fmt.Fprintf(w, "%d existing thread(s) read, none carrying a fingerprint this run can match. Nothing could be withheld on identity.\n\n", read)
		} else {
			fmt.Fprintf(w, "%d existing thread(s) read, %d carrying a fingerprint this run can match.\n\n", read, identified)
		}
	}
	if n := len(r.Suppressed); n > 0 {
		fmt.Fprintf(w, "Already on the pull request (%d), so not posted again:\n", n)
		for _, s := range r.Suppressed {
			fmt.Fprintf(w, "  - %s  %s — %s\n", location(s.Finding), s.Finding.Category, s.Reason)
		}
		fmt.Fprintln(w)
	}
	if other := r.Threads.AtOtherVersion(); len(other) > 0 {
		fmt.Fprintf(w, "%d existing thread(s) carry a fingerprint from another scheme version, which identifies nothing this run computes; a finding matching one is posted again.\n\n",
			len(other))
	}
}

// findingsAt returns the findings carrying one severity, in report order.
func findingsAt(findings []Finding, sev Severity) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Severity == sev {
			out = append(out, f)
		}
	}
	return out
}

// location renders where a finding is.
func location(f Finding) string {
	if f.Path == "" {
		return "(no file)"
	}
	if !f.HasLine() {
		return f.Path
	}
	return f.Path + ":" + lineRange(f)
}

// severityGloss says what a severity obliges.
func severityGloss(s Severity) string {
	switch s {
	case SeverityRed:
		return "must fix before merge"
	case SeverityAmber:
		return "should fix"
	case SeverityGreen:
		return "nice to have"
	}
	return ""
}

// RenderPlan writes what a review would do and what it would send.
func RenderPlan(w io.Writer, p *Plan) {
	fmt.Fprintf(w, "manifest: %s\n", p.Manifest)
	fmt.Fprintf(w, "range:    %s\n", p.Range)
	fmt.Fprintf(w, "panel:    %s\n", p.Panel)
	fmt.Fprintf(w, "root:     %s\n", p.Material.Root.Code)
	fmt.Fprintf(w, "workdir:  %s\n", p.Material.Root.Work)
	if docs := p.Material.ConventionPaths(); len(docs) > 0 {
		fmt.Fprintf(w, "rules:    %s\n", strings.Join(docs, ", "))
	} else {
		fmt.Fprintf(w, "rules:    none found at the base ref\n")
	}
	if len(p.MissingConventions) > 0 {
		fmt.Fprintf(w, "missing: %s (named by the manifest, absent at the base ref)\n",
			strings.Join(p.MissingConventions, ", "))
	}
	fmt.Fprintf(w, "\n%d run(s) would be made, and nothing was spent:\n", len(p.Runs))
	fmt.Fprintf(w, "One validator run is added per distinct candidate finding, which is not known until the reviewers answer.\n\n")
	for _, run := range p.Runs {
		model := run.Model
		if model == "" {
			model = "the CLI's own default"
		}
		fmt.Fprintf(w, "  %s (%s) — %s, %s\n", run.Label, run.Role, run.Provider, model)
	}
	for _, run := range p.Runs {
		fmt.Fprintf(w, "\n%s\n=== prompt for %s (%d bytes) ===\n%s\n",
			strings.Repeat("-", 72), run.Label, len(run.Prompt), run.Prompt)
	}
}
