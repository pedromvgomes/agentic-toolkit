package reviewrun

import (
	"strings"
	"testing"
)

// The whole point of the shape: a caller cannot get the slice without also
// being handed the answer to "did this run answer at all".
func TestAnUnavailableReportYieldsNoFindingsAndSaysSo(t *testing.T) {
	r := Unavailable("the CLI was not installed")

	findings, ok := r.Findings()
	if ok {
		t.Fatal("an unavailable report reports itself as available")
	}
	if findings != nil {
		t.Errorf("an unavailable report handed out findings: %+v", findings)
	}
	if r.Count() != 0 {
		t.Errorf("an unavailable report counts %d findings", r.Count())
	}
	if !strings.Contains(r.String(), "could not answer") {
		t.Errorf("an unavailable report renders as %q", r.String())
	}
}

// A run that answered and found nothing is a clean review, and must be
// distinguishable from one that could not look.
func TestAnAnsweredEmptyReportIsAvailable(t *testing.T) {
	findings, ok := Answered(nil).Findings()
	if !ok {
		t.Fatal("a run that found nothing reports itself as unavailable")
	}
	if len(findings) != 0 {
		t.Errorf("an empty answer carried %d findings", len(findings))
	}
}

func TestAnAnsweredReportCarriesItsFindings(t *testing.T) {
	r := Answered([]Finding{{Path: "a.go"}, {Path: "b.go"}})
	findings, ok := r.Findings()
	if !ok || len(findings) != 2 {
		t.Fatalf("Findings() = %+v, %v", findings, ok)
	}
	if r.Count() != 2 {
		t.Errorf("Count() = %d", r.Count())
	}
	if !strings.Contains(r.String(), "2 findings") {
		t.Errorf("String() = %q", r.String())
	}
}

func TestUnavailableFormatsItsReason(t *testing.T) {
	if got := Unavailable("%s exited %d", "codex", 2).Reason; got != "codex exited 2" {
		t.Errorf("reason is %q", got)
	}
}

// A review that reached a verdict on three reviewers out of four is still a
// verdict, and the reader has to be told which quarter is missing.
func TestPartialAndUnansweredNameTheRunsThatFailed(t *testing.T) {
	r := &Review{Reports: []RunReport{
		{Label: "correctness", Role: RoleReviewer, Report: Answered([]Finding{{Path: "a.go"}})},
		{Label: "security", Role: RoleReviewer, Report: Unavailable("timed out")},
	}}
	if !r.Partial() {
		t.Error("a review with an unanswered run does not report itself as partial")
	}
	unanswered := r.Unanswered()
	if len(unanswered) != 1 || unanswered[0].Label != "security" {
		t.Fatalf("Unanswered() = %+v", unanswered)
	}
}

func TestAReviewWhereEveryRunAnsweredIsNotPartial(t *testing.T) {
	r := &Review{Reports: []RunReport{
		{Label: "correctness", Role: RoleReviewer, Report: Answered(nil)},
	}}
	if r.Partial() {
		t.Error("a complete review reports itself as partial")
	}
	if len(r.Unanswered()) != 0 {
		t.Errorf("Unanswered() = %+v", r.Unanswered())
	}
}

// A reviewer that ran and found nothing and one that never ran look identical
// in a table that only lists findings, and they mean opposite things.
func TestSilentNamesReviewersThatRanAndFoundNothing(t *testing.T) {
	r := &Review{Reports: []RunReport{
		{Label: "correctness", Role: RoleReviewer, Report: Answered(nil)},
		{Label: "security", Role: RoleReviewer, Report: Answered([]Finding{{Path: "a.go"}})},
		{Label: "performance", Role: RoleReviewer, Report: Unavailable("outage")},
		{Label: "judge", Role: RoleJudge, Report: Answered(nil)},
	}}
	silent := r.Silent()
	if len(silent) != 1 || silent[0].Label != "correctness" {
		t.Fatalf("Silent() = %+v; a run that could not answer and the judge must not be in it", silent)
	}
}

func TestCountsGroupsFindingsBySeverity(t *testing.T) {
	r := &Review{Findings: []Finding{
		{Severity: SeverityRed}, {Severity: SeverityRed}, {Severity: SeverityAmber},
	}}
	counts := r.Counts()
	if counts[SeverityRed] != 2 || counts[SeverityAmber] != 1 || counts[SeverityGreen] != 0 {
		t.Errorf("Counts() = %+v", counts)
	}
}

func TestTheRecordReportsWhatRanAndWhatItCost(t *testing.T) {
	r := &Review{
		Panel: "deep",
		Reports: []RunReport{
			{Label: "a", Report: Answered(nil)},
			{Label: "b", Report: Unavailable("outage")},
		},
		DroppedByValidator: 3,
		Conventions:        []string{"CLAUDE.md", "CONTEXT.md"},
		CostUSD:            0.1234,
	}
	got := r.Record()
	for _, want := range []string{"panel deep", "2 runs", "1 could not answer",
		"3 dropped by a validator", "2 convention docs read", "$0.1234"} {
		if !strings.Contains(got, want) {
			t.Errorf("the record omits %q: %q", want, got)
		}
	}
}

// A review that spent nothing says nothing about cost rather than claiming $0,
// which would read as a real measurement.
func TestTheRecordOmitsWhatDidNotHappen(t *testing.T) {
	r := &Review{Panel: "quick", Reports: []RunReport{{Label: "a", Report: Answered(nil)}}}
	got := r.Record()
	for _, unwanted := range []string{"could not answer", "dropped", "convention", "$"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the record claims %q on a clean run: %q", unwanted, got)
		}
	}
}
