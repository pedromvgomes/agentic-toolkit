package reviewrun

import (
	"strings"
	"testing"
)

func ptr(n int) *int { return &n }

func TestSeverityRanksMostSeriousFirst(t *testing.T) {
	if SeverityRed.Rank() >= SeverityAmber.Rank() || SeverityAmber.Rank() >= SeverityGreen.Rank() {
		t.Fatalf("the ladder is out of order: %d %d %d",
			SeverityRed.Rank(), SeverityAmber.Rank(), SeverityGreen.Rank())
	}
}

// A severity nobody defined sorts last. "I do not know how bad this is" must
// not be read as "worst", which is what promoting an unknown value to the top
// of the table would do.
func TestAnUnknownSeveritySortsLastAndIsInvalid(t *testing.T) {
	unknown := Severity("CRITICAL")
	if unknown.Valid() {
		t.Error("an invented severity reports itself as valid")
	}
	if unknown.Rank() <= SeverityGreen.Rank() {
		t.Errorf("an invented severity outranks GREEN: %d vs %d", unknown.Rank(), SeverityGreen.Rank())
	}
}

func TestNormaliseEvidenceTakesTheFirstNonEmptyLineAndCollapsesSpace(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"  \n\n\tif  x   ==  nil {\nreturn\n", "if x == nil {"},
		{"if x == nil {", "if x == nil {"},
		{"\n\n", ""},
		{"", ""},
		{"   ", ""},
	} {
		if got := normaliseEvidence(tc.in); got != tc.want {
			t.Errorf("normaliseEvidence(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Identity is the quoted code, not the line and not the prose. A finding that
// moved down the file and was described differently is the same finding.
func TestTheFingerprintIgnoresLineAndProse(t *testing.T) {
	a := Finding{Path: "a.go", Category: "correctness", Evidence: "x := 1", StartLine: ptr(10), Issue: "one wording"}
	b := Finding{Path: "a.go", Category: "correctness", Evidence: "  x   :=  1  ", StartLine: ptr(90), Issue: "quite another wording"}
	if a.Fingerprint() != b.Fingerprint() {
		t.Errorf("re-indented and moved code produced a different fingerprint: %s vs %s",
			a.Fingerprint(), b.Fingerprint())
	}
}

// Fixed code is a different fingerprint, which is what stops a resolved thread
// from suppressing a fix that did not work.
func TestTheFingerprintChangesWithTheQuotedCode(t *testing.T) {
	a := Finding{Path: "a.go", Category: "correctness", Evidence: "x := 1"}
	b := Finding{Path: "a.go", Category: "correctness", Evidence: "x := 2"}
	if a.Fingerprint() == b.Fingerprint() {
		t.Error("different code produced the same fingerprint")
	}
}

func TestTheFingerprintSeparatesPathAndCategory(t *testing.T) {
	base := Finding{Path: "a.go", Category: "correctness", Evidence: "x := 1"}
	other := base
	other.Path = "b.go"
	if base.Fingerprint() == other.Fingerprint() {
		t.Error("the same code in two files shares a fingerprint")
	}
	other = base
	other.Category = "security"
	if base.Fingerprint() == other.Fingerprint() {
		t.Error("two kinds of problem about the same code share a fingerprint")
	}
}

func TestTheFingerprintIsTheAgreedWidth(t *testing.T) {
	f := Finding{Path: "a.go", Category: "c", Evidence: "x"}
	if got := f.Fingerprint(); len(got) != fingerprintWidth {
		t.Errorf("fingerprint %q is %d characters, want %d", got, len(got), fingerprintWidth)
	}
}

// Corroboration counts instances that agreed, not findings filed: one instance
// repeating itself has corroborated nothing.
func TestCorroborationCountsInstancesNotFindings(t *testing.T) {
	same := Finding{Path: "a.go", Category: "correctness", Evidence: "x := 1", Severity: SeverityAmber}
	got := corroborate([][]Finding{
		{same, same},
		{same},
	})
	if len(got) != 1 {
		t.Fatalf("want one distinct claim, got %d", len(got))
	}
	if got[0].Corroboration != 2 {
		t.Errorf("corroboration is %d, want 2 (two instances, one of which said it twice)", got[0].Corroboration)
	}
}

func TestCorroborationKeepsDistinctClaimsApart(t *testing.T) {
	a := Finding{Path: "a.go", Category: "correctness", Evidence: "x := 1", Severity: SeverityAmber}
	b := Finding{Path: "b.go", Category: "correctness", Evidence: "y := 2", Severity: SeverityAmber}
	got := corroborate([][]Finding{{a}, {b}})
	if len(got) != 2 {
		t.Fatalf("two claims folded into %d", len(got))
	}
	for _, f := range got {
		if f.Corroboration != 1 {
			t.Errorf("%s is corroborated %d times", f.Path, f.Corroboration)
		}
	}
}

// Where two instances disagree about how bad one defect is, the more serious
// reading reaches the judge — which is the run whose job is to decide it.
func TestCorroborationKeepsTheMoreSeriousSeverity(t *testing.T) {
	mild := Finding{Path: "a.go", Category: "c", Evidence: "x := 1", Severity: SeverityGreen}
	grave := Finding{Path: "a.go", Category: "c", Evidence: "x := 1", Severity: SeverityRed}
	got := corroborate([][]Finding{{mild}, {grave}})
	if len(got) != 1 || got[0].Severity != SeverityRed {
		t.Fatalf("want one RED, got %+v", got)
	}
}

func TestCorroborationOfNothingIsEmpty(t *testing.T) {
	if got := corroborate(nil); len(got) != 0 {
		t.Errorf("corroborate(nil) = %+v", got)
	}
	if got := corroborate([][]Finding{{}, {}}); len(got) != 0 {
		t.Errorf("corroborate of two empty instances = %+v", got)
	}
}

func TestSortingPutsSeriousFirstThenFileThenLine(t *testing.T) {
	findings := []Finding{
		{Severity: SeverityGreen, Path: "a.go", StartLine: ptr(1)},
		{Severity: SeverityRed, Path: "b.go", StartLine: ptr(5)},
		{Severity: SeverityRed, Path: "a.go", StartLine: ptr(9)},
		{Severity: SeverityRed, Path: "a.go", StartLine: ptr(2)},
		{Severity: SeverityAmber, Path: "z.go"},
	}
	sortFindings(findings)

	var got []string
	for _, f := range findings {
		got = append(got, string(f.Severity)+" "+f.Path+":"+lineRange(f))
	}
	want := []string{
		"RED a.go:2", "RED a.go:9", "RED b.go:5",
		"AMBER z.go:cross-cutting (no line)",
		"GREEN a.go:1",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d is %q, want %q (whole order: %v)", i, got[i], want[i], got)
		}
	}
}

func TestAFindingWithNoValidatorIsUpheld(t *testing.T) {
	if !(Finding{}).Upheld() {
		t.Error("a finding nobody validated reads as rejected")
	}
}

func TestARejectedFindingIsNotUpheld(t *testing.T) {
	f := Finding{Verdict: &Verdict{Verdict: VerdictRejected}}
	if f.Upheld() {
		t.Error("a rejected finding reads as upheld")
	}
	for _, v := range []string{VerdictUpheld, VerdictDowngraded} {
		f := Finding{Verdict: &Verdict{Verdict: v}}
		if !f.Upheld() {
			t.Errorf("a %s finding reads as rejected", v)
		}
	}
}

func TestHasLineDistinguishesInlineFromCrossCutting(t *testing.T) {
	if (Finding{}).HasLine() {
		t.Error("a finding with no line claims to have one")
	}
	if !(Finding{StartLine: ptr(3)}).HasLine() {
		t.Error("a finding with a line claims not to")
	}
}

func TestLineRangeRendersEveryShape(t *testing.T) {
	for _, tc := range []struct {
		f    Finding
		want string
	}{
		{Finding{}, "cross-cutting (no line)"},
		{Finding{StartLine: ptr(4)}, "4"},
		{Finding{StartLine: ptr(4), EndLine: ptr(4)}, "4"},
		{Finding{StartLine: ptr(4), EndLine: ptr(9)}, "4-9"},
	} {
		if got := lineRange(tc.f); got != tc.want {
			t.Errorf("lineRange(%+v) = %q, want %q", tc.f, got, tc.want)
		}
	}
}

func TestAssignIDsNumbersEveryCandidate(t *testing.T) {
	got := assignIDs([]Finding{{Path: "a"}, {Path: "b"}, {Path: "c"}})
	for i, want := range []string{"f1", "f2", "f3"} {
		if got[i].ID != want {
			t.Errorf("candidate %d has id %q, want %q", i, got[i].ID, want)
		}
	}
}

func TestInstanceLabelNumbersOnlyUnderAQuorum(t *testing.T) {
	if got := instanceLabel("security", 1, 1); got != "security" {
		t.Errorf("a single run is labelled %q", got)
	}
	if got := instanceLabel("security", 2, 3); got != "security#2" {
		t.Errorf("an instance under quorum is labelled %q", got)
	}
	if got := reviewerOf("security#2"); got != "security" {
		t.Errorf("reviewerOf lost the name: %q", got)
	}
	if got := reviewerOf("security"); got != "security" {
		t.Errorf("reviewerOf mangled an unnumbered label: %q", got)
	}
}

func TestACandidateFindingCarriesItsEvidenceAndCorroboration(t *testing.T) {
	f := Finding{
		ID: "f1", Reviewer: "security", Path: "a.go", StartLine: ptr(3), EndLine: ptr(5),
		Category: "security:injection", Severity: SeverityRed, Confidence: "high",
		Issue: "unparameterised query", Evidence: "db.Query(\"select \" + name)",
		Corroboration: 2, Verdict: &Verdict{Verdict: VerdictUpheld, Reason: "confirmed"},
	}
	got := renderCandidateFinding(f, true)
	for _, want := range []string{"## f1", "a.go", "3-5", "security:injection", "RED",
		"2 reviewer instance", "upheld", "db.Query"} {
		if !strings.Contains(got, want) {
			t.Errorf("the candidate rendering omits %q:\n%s", want, got)
		}
	}
}

// A validator is judging a claim, not answering about an identified one, so it
// is not handed the id or the corroboration count — either would tell it how
// many others already agreed, which is exactly the independence it supplies.
func TestAValidatorIsNotShownTheIDOrHowManyOthersAgreed(t *testing.T) {
	f := Finding{ID: "f1", Path: "a.go", Evidence: "x", Corroboration: 3}
	got := renderCandidateFinding(f, false)
	if strings.Contains(got, "## f1") {
		t.Errorf("a validator is shown the id:\n%s", got)
	}
	if strings.Contains(got, "reviewer instance") {
		t.Errorf("a validator is told how many others agreed:\n%s", got)
	}
}
