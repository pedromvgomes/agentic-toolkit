package reviewrun

import (
	"strings"
	"testing"
)

// head is a commit id a marker is bound to.
const head = "0123456789abcdef0123456789abcdef01234567"

// marked builds a marker carrying one finding.
func marked(fingerprint string, severity Severity, answerable, injected bool) ReviewMarker {
	return ReviewMarker{
		Head:     head,
		Complete: true,
		Findings: []MarkedFinding{{
			Fingerprint: fingerprint, Severity: severity,
			Answerable: answerable, Injected: injected,
		}},
	}
}

// Nothing is persisted between runs, so a review's own body is the only record
// of what it found. Everything approval decides from has to survive the round
// trip through it.
func TestAReviewMarkerSurvivesBeingWrittenAndReadBack(t *testing.T) {
	want := ReviewMarker{
		Head:     head,
		Complete: true,
		Findings: []MarkedFinding{
			{Fingerprint: "aaaaaaaaaaaa", Severity: SeverityRed, Answerable: true},
			{Fingerprint: "bbbbbbbbbbbb", Severity: SeverityAmber, Answerable: false},
			{Fingerprint: "cccccccccccc", Severity: SeverityGreen, Answerable: true},
			{Fingerprint: "dddddddddddd", Severity: SeverityRed, Answerable: false, Injected: true},
		},
	}
	got, ok := ParseReviewMarker("## a review\n\n" + want.Render() + "\n")
	if !ok {
		t.Fatalf("the marker this build wrote does not parse:\n%s", want.Render())
	}
	if got.Head != want.Head || got.Complete != want.Complete {
		t.Fatalf("read head=%q complete=%v, want %q and %v", got.Head, got.Complete, want.Head, want.Complete)
	}
	if len(got.Findings) != len(want.Findings) {
		t.Fatalf("read %d findings, want %d: %+v", len(got.Findings), len(want.Findings), got.Findings)
	}
	for i, f := range want.Findings {
		if got.Findings[i] != f {
			t.Errorf("finding %d read back as %+v, want %+v", i, got.Findings[i], f)
		}
	}
}

// A run that could not reach a verdict found less than it would have, and
// "found nothing" is the one thing approval must never read that as.
func TestAnIncompleteVerdictIsCarriedAndReadBack(t *testing.T) {
	body := ReviewMarker{Head: head}.Render()
	if !strings.Contains(body, "verdict="+VerdictIncomplete) {
		t.Fatalf("an incomplete run does not say so:\n%s", body)
	}
	got, ok := ParseReviewMarker(body)
	if !ok {
		t.Fatal("the marker does not parse")
	}
	if got.Complete {
		t.Error("a run that reached no verdict reads back as one that did")
	}
}

// agtk writes exactly one marker and escapes the opening delimiter everywhere
// a model's own words are rendered, so a second one is something agtk did not
// write — and a reader that chose between them would be choosing which claim
// about a commit to believe.
func TestABodyCarryingTwoMarkersIsRefused(t *testing.T) {
	one := marked("aaaaaaaaaaaa", SeverityRed, true, false)
	two := marked("bbbbbbbbbbbb", SeverityGreen, true, false)
	if _, ok := ParseReviewMarker(one.Render() + "\ntext\n" + two.Render()); ok {
		t.Error("a body carrying two markers was resolved by picking one")
	}
}

// A marker approval cannot read refuses rather than approves, so every
// malformed shape has to fail closed rather than parse into something partial.
func TestAMalformedMarkerIsNotRead(t *testing.T) {
	for name, body := range map[string]string{
		"no marker at all":   "## a review\n\nnothing here\n",
		"another scheme":     "<!-- agtk:review v9 head=" + head + " verdict=complete -->",
		"no head":            "<!-- agtk:review " + FingerprintVersion + " verdict=complete -->",
		"no verdict":         "<!-- agtk:review " + FingerprintVersion + " head=" + head + " -->",
		"an unknown verdict": "<!-- agtk:review " + FingerprintVersion + " head=" + head + " verdict=maybe -->",
		"a severity off the ladder": "<!-- agtk:review " + FingerprintVersion + " head=" + head +
			" verdict=complete aaaaaaaaaaaa=PUCE -->",
		"a token that is not a pair": "<!-- agtk:review " + FingerprintVersion + " head=" + head +
			" verdict=complete orphan -->",
		"never closed": "<!-- agtk:review " + FingerprintVersion + " head=" + head + " verdict=complete",
	} {
		if _, ok := ParseReviewMarker(body); ok {
			t.Errorf("%s: was read as a marker", name)
		}
	}
}

// A finding agtk could give nobody a thread to answer on blocks nothing, and
// the marker is the only thing that says so — approval reading it as
// answerable would demand an answer nobody can write.
func TestAnUnanswerableFindingIsRecordedAsOne(t *testing.T) {
	body := marked("aaaaaaaaaaaa", SeverityRed, false, false).Render()
	if !strings.Contains(body, "unanswerable=aaaaaaaaaaaa") {
		t.Fatalf("the marker does not record it as unanswerable:\n%s", body)
	}
	got, _ := ParseReviewMarker(body)
	if got.Findings[0].Answerable {
		t.Error("an unanswerable finding reads back as answerable")
	}
	if got.Findings[0].Injected {
		t.Error("an ordinary finding reads back as an injection finding")
	}
}

// A prompt-injection finding agtk could not attach refuses approval outright,
// so it is recorded apart from the findings that merely gate nothing.
func TestAnUnattachedInjectionFindingIsRecordedAsADeadlock(t *testing.T) {
	body := marked("dddddddddddd", SeverityRed, false, true).Render()
	if !strings.Contains(body, "deadlocked=dddddddddddd") {
		t.Fatalf("the marker does not record the deadlock:\n%s", body)
	}
	if strings.Contains(body, "unanswerable=dddddddddddd") {
		t.Errorf("a deadlocked finding is also listed as merely unanswerable:\n%s", body)
	}
	got, _ := ParseReviewMarker(body)
	if got.Findings[0].Answerable || !got.Findings[0].Injected {
		t.Errorf("read back as %+v, want unanswerable and injected", got.Findings[0])
	}
}

// An injection finding agtk COULD attach is answerable: there is a thread, and
// a written statement on it clears the finding like any other.
func TestAnAttachedInjectionFindingIsNotADeadlock(t *testing.T) {
	body := marked("dddddddddddd", SeverityRed, true, true).Render()
	if strings.Contains(body, "deadlocked=") {
		t.Fatalf("an injection finding with a thread was recorded as a deadlock:\n%s", body)
	}
	got, _ := ParseReviewMarker(body)
	if !got.Findings[0].Answerable {
		t.Error("an injection finding with a thread reads back as unanswerable")
	}
}

// The review marker and the fingerprint marker are two formats in one body.
// A reader looking for one must never match the other.
func TestTheTwoMarkersDoNotReadEachOther(t *testing.T) {
	body := FingerprintMarker("aaaaaaaaaaaa") + "\n" + marked("bbbbbbbbbbbb", SeverityRed, true, false).Render()

	if _, _, ok := ParseFingerprintMarker(body); !ok {
		t.Error("the fingerprint marker is unreadable beside a review marker")
	}
	got, ok := ParseReviewMarker(body)
	if !ok {
		t.Fatal("the review marker is unreadable beside a fingerprint marker")
	}
	if len(got.Findings) != 1 || got.Findings[0].Fingerprint != "bbbbbbbbbbbb" {
		t.Errorf("the review marker read %+v; it took the fingerprint marker's identity", got.Findings)
	}
}
