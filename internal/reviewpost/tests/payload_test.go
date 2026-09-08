package tests

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewpost"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun"
)

func at(n int) *int { return &n }

// pr is the pull request every payload here is built against.
var pr = githubapp.PullRequest{
	Number: 7, BaseRef: "main", HeadRef: "feature/x",
	BaseSHA: strings.Repeat("1", 40), HeadSHA: strings.Repeat("2", 40),
}

// added is a diff that adds lines 10, 11 and 12 of one file and line 3 of
// another, and nothing else.
var added = reviewpost.AddedLines{
	"a.go": {10: true, 11: true, 12: true},
	"b.go": {3: true},
}

func finding(path string, start, end *int, category string) reviewrun.Finding {
	return reviewrun.Finding{
		ID: "f1", Reviewer: "correctness", Path: path,
		StartLine: start, EndLine: end,
		Category: category, Severity: reviewrun.SeverityRed,
		Issue: "the bound is off by one", Evidence: "for i := 0; i <= len(x); i++ {",
		Suggestion: "use <", Corroboration: 2,
	}
}

// reviewWith builds a review that reached a verdict over a pull request whose
// comment threads were read and held nothing. Threads travel with it because a
// thread list that could not be read is a review that found less than it would
// have, which the posted marker has to record.
func reviewWith(findings ...reviewrun.Finding) *reviewrun.Review {
	return &reviewrun.Review{
		Panel: "standard", Manifest: "builtin", Range: "main..HEAD",
		Available: true, Findings: findings, Threads: reviewrun.ThreadsRead(nil),
		Reports: []reviewrun.RunReport{{
			Label: "correctness", Role: reviewrun.RoleReviewer,
			Provider: "claudecode", Report: reviewrun.Answered(findings),
		}},
	}
}

// A finding on a line the change adds is the ordinary case, and it is the only
// one that becomes an inline comment.
func TestAFindingOnTheDiffBecomesAnInlineComment(t *testing.T) {
	r := reviewWith(finding("a.go", at(10), at(10), "correctness"))
	payload, place := reviewpost.Build(r, pr, added)

	if len(payload.Comments) != 1 || len(place.Inline) != 1 {
		t.Fatalf("%d comments, %d inline", len(payload.Comments), len(place.Inline))
	}
	c := payload.Comments[0]
	if c.Path != "a.go" || c.Line != 10 || c.Side != githubapp.SideRight {
		t.Errorf("the comment is %+v", c)
	}
	if c.StartLine != nil {
		t.Error("a single-line finding asked for a multi-line comment, which GitHub refuses when start equals line")
	}
	if payload.CommitID != pr.HeadSHA {
		t.Errorf("the review is bound to %q, want the head it was made against", payload.CommitID)
	}
	if payload.Event != githubapp.EventComment {
		t.Errorf("the review posts event %q", payload.Event)
	}
}

// Identity across runs is the fingerprint, and a later run reads it back off
// the pull request rather than re-deriving it — so the marker has to be the
// one this finding actually hashes to.
func TestEveryInlineCommentCarriesItsOwnFingerprint(t *testing.T) {
	f := finding("a.go", at(11), at(11), "correctness")
	payload, _ := reviewpost.Build(reviewWith(f), pr, added)
	want := "<!-- agtk:finding v1 " + f.Fingerprint() + " -->"
	body := payload.Comments[0].Body
	if !strings.Contains(body, want) {
		t.Fatalf("the comment does not carry %s:\n%s", want, body)
	}
	// The version is what separates "the scheme moved" from "every finding is
	// new"; a marker without it is worse than no marker.
	if !strings.Contains(want, reviewrun.FingerprintVersion) {
		t.Errorf("the marker carries no scheme version: %s", want)
	}
}

// A marker is an HTML comment so it is invisible where a person reads it, and
// visible where a later run looks for it.
func TestTheMarkerIsAnHTMLCommentAndNothingElse(t *testing.T) {
	marker := reviewpost.FingerprintMarker("cdbb1d5c5dec")
	if !strings.HasPrefix(marker, "<!--") || !strings.HasSuffix(marker, "-->") {
		t.Fatalf("the marker renders visibly: %q", marker)
	}
	if strings.Count(marker, "-->") != 1 {
		t.Errorf("the marker closes more than once: %q", marker)
	}
}

// A finding that names no file at all can be given no thread: there is nothing
// for GitHub to hang a comment off. It must still reach the pull request.
func TestAFindingWithNoPathIsStatedInTheBody(t *testing.T) {
	f := finding("", nil, nil, "architecture")
	f.Issue = "the two provider tables have drifted apart"
	payload, place := reviewpost.Build(reviewWith(f), pr, added)

	if len(payload.Comments) != 0 {
		t.Fatalf("a finding with no path became an inline comment: %+v", payload.Comments)
	}
	if len(place.Unattachable) != 1 {
		t.Fatalf("the finding was placed as %+v", place)
	}
	if !strings.Contains(payload.Body, "the two provider tables have drifted apart") {
		t.Errorf("the finding reaches the pull request nowhere:\n%s", payload.Body)
	}
}

// A claim about a whole file has no line to be an inline comment on, and the
// change touches its path — so it gets a comment against the file, which is a
// thread somebody can answer on.
func TestAFindingWithNoLineOnATouchedPathBecomesAFileComment(t *testing.T) {
	f := finding("a.go", nil, nil, "architecture")
	f.Issue = "this file has two reasons to change"
	payload, place := reviewpost.Build(reviewWith(f), pr, added)

	if len(payload.Comments) != 0 {
		t.Fatalf("a finding with no line became an inline comment: %+v", payload.Comments)
	}
	if len(place.FileLevel) != 1 || len(place.Unattachable) != 0 {
		t.Fatalf("the finding was placed as %+v", place)
	}
	comments := reviewpost.FileComments(pr, place)
	if len(comments) != 1 || comments[0].Path != "a.go" {
		t.Fatalf("the file-level comments are %+v", comments)
	}
	if comments[0].CommitID != pr.HeadSHA {
		t.Errorf("the comment is bound to %q, want the head it describes", comments[0].CommitID)
	}
	if !strings.Contains(comments[0].Body, "this file has two reasons to change") {
		t.Errorf("the comment does not carry the finding:\n%s", comments[0].Body)
	}
	if !strings.Contains(comments[0].Body, reviewpost.FingerprintMarker(f.Fingerprint())) {
		t.Errorf("a file-level comment carries no identity, so approval cannot find its thread:\n%s", comments[0].Body)
	}
}

// One comment on a line outside the diff rejects the entire review, discarding
// a panel that has already been paid for — including every comment that was
// right. The finding still needs a thread, so it hangs off the whole file.
func TestAFindingOffTheDiffBecomesAFileCommentRatherThanBeingSentInline(t *testing.T) {
	off := finding("a.go", at(40), at(40), "correctness")
	off.Issue = "this loop never terminates"
	on := finding("b.go", at(3), at(3), "security")

	payload, place := reviewpost.Build(reviewWith(off, on), pr, added)

	if len(payload.Comments) != 1 || payload.Comments[0].Path != "b.go" {
		t.Fatalf("the comment list is %+v; a line outside the diff was sent", payload.Comments)
	}
	if len(place.FileLevel) != 1 || len(place.Unattachable) != 0 {
		t.Fatalf("the off-diff finding was placed as %+v", place)
	}
	comments := reviewpost.FileComments(pr, place)
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "this loop never terminates") {
		t.Errorf("a finding that could not be positioned got no thread: %+v", comments)
	}
}

// GitHub refuses a comment on a path the change does not touch, so agtk can
// offer nobody a thread to answer on — which is exactly what mechanical
// exclusions make likely.
func TestAFindingInAFileTheChangeDoesNotTouchGetsNoThread(t *testing.T) {
	f := finding("vendor/z.go", at(3), at(3), "performance")
	payload, place := reviewpost.Build(reviewWith(f), pr, added)
	if len(payload.Comments) != 0 || len(place.Unattachable) != 1 {
		t.Fatalf("%d comments, placed as %+v", len(payload.Comments), place)
	}
	if len(reviewpost.FileComments(pr, place)) != 0 {
		t.Error("a comment was built for a path GitHub refuses")
	}
}

// GitHub validates the whole span of a multi-line comment, so a region
// reaching back across unchanged code is refused.
func TestAMultiLineCommentIsAskedForOnlyWhenTheWholeSpanIsOnTheDiff(t *testing.T) {
	whole := finding("a.go", at(10), at(12), "correctness")
	payload, _ := reviewpost.Build(reviewWith(whole), pr, added)
	c := payload.Comments[0]
	if c.StartLine == nil || *c.StartLine != 10 || c.Line != 12 {
		t.Fatalf("a fully added region did not become a multi-line comment: %+v", c)
	}
	if c.StartSide == nil || *c.StartSide != githubapp.SideRight {
		t.Errorf("the span's start is on %v, want the head side", c.StartSide)
	}

	// Line 9 is not on the diff, so the span cannot be.
	straddling := finding("a.go", at(9), at(11), "correctness")
	payload, _ = reviewpost.Build(reviewWith(straddling), pr, added)
	c = payload.Comments[0]
	if c.StartLine != nil {
		t.Errorf("a span reaching across unchanged code was sent as multi-line: %+v", c)
	}
	if c.Line != 11 {
		t.Errorf("the comment anchored at %d, want the end of the region", c.Line)
	}
}

// The anchor is the end of the region, which is the line GitHub validates: a
// finding beginning on the diff and ending off it is refused.
func TestAFindingEndingOffTheDiffIsNotSentEvenWhenItBeginsOnIt(t *testing.T) {
	f := finding("a.go", at(12), at(20), "correctness")
	payload, place := reviewpost.Build(reviewWith(f), pr, added)
	if len(payload.Comments) != 0 {
		t.Fatalf("a region ending outside the diff was sent: %+v", payload.Comments)
	}
	if len(place.FileLevel) != 1 {
		t.Errorf("placed as %+v", place)
	}
}

// A review reporting nothing looks exactly like a clean review, and a clean
// review is what unblocks approval. A reader has to be able to tell them
// apart from the body alone.
func TestABodyDistinguishesACleanReviewFromAPartialOne(t *testing.T) {
	clean := reviewWith()
	payload, _ := reviewpost.Build(clean, pr, added)
	if !strings.Contains(payload.Body, "No findings survived the panel") {
		t.Errorf("a review that found nothing does not say so:\n%s", payload.Body)
	}
	if strings.Contains(payload.Body, "partial") {
		t.Errorf("a complete review calls itself partial:\n%s", payload.Body)
	}

	partial := reviewWith()
	partial.Reports = append(partial.Reports, reviewrun.RunReport{
		Label: "security", Role: reviewrun.RoleReviewer, Provider: "claudecode",
		Report: reviewrun.Unavailable("the CLI is not installed"),
	})
	payload, _ = reviewpost.Build(partial, pr, added)
	if !strings.Contains(payload.Body, "partial") {
		t.Errorf("a review a quarter of whose panel never ran reads as clean:\n%s", payload.Body)
	}
	if !strings.Contains(payload.Body, "the CLI is not installed") {
		t.Errorf("the body does not say why a run could not answer:\n%s", payload.Body)
	}
}

// A reviewer that ran and found nothing and one that never ran are the same
// shape in a body that lists only findings, and they mean opposite things.
func TestABodyNamesTheReviewersThatRanAndHadNoOpinion(t *testing.T) {
	r := reviewWith()
	payload, _ := reviewpost.Build(r, pr, added)
	if !strings.Contains(payload.Body, "Ran and reported nothing") {
		t.Errorf("a silent reviewer is absent from the body rather than named in it:\n%s", payload.Body)
	}
}

// A review that did not reach a verdict must not be posted as one that found
// nothing.
func TestABodyForAReviewWithNoVerdictSaysSoFirst(t *testing.T) {
	r := reviewWith()
	r.Available = false
	r.Reason = "the judge could not be run"
	payload, _ := reviewpost.Build(r, pr, added)
	if !strings.Contains(payload.Body, "did not reach a verdict") {
		t.Errorf("a review with no verdict reads as a clean one:\n%s", payload.Body)
	}
	if strings.Contains(payload.Body, "No findings survived") {
		t.Errorf("a review with no verdict claims the panel found nothing:\n%s", payload.Body)
	}
}

// Whatever else the body carries, it names the commit and what ran, because a
// review is evidence that a particular commit was looked at.
func TestABodyCarriesTheReviewRecord(t *testing.T) {
	payload, _ := reviewpost.Build(reviewWith(), pr, added)
	for _, want := range []string{"standard", "main..HEAD"} {
		if !strings.Contains(payload.Body, want) {
			t.Errorf("the body does not carry %q:\n%s", want, payload.Body)
		}
	}
}

// The comment list travels as JSON, and GitHub reads a null there as a
// malformed field rather than as an empty one.
func TestACommentListWithNothingInItSerialisesAsAnEmptyList(t *testing.T) {
	payload, _ := reviewpost.Build(reviewWith(), pr, added)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var sent map[string]json.RawMessage
	if err := json.Unmarshal(raw, &sent); err != nil {
		t.Fatal(err)
	}
	if string(sent["comments"]) != "[]" {
		t.Errorf("comments serialises as %s, want []", sent["comments"])
	}
}

// A comment says who reported the claim and what happened to it, so a reader
// can weigh one reviewer's lone finding against one three reached
// independently.
func TestACommentSaysWhoReportedItAndWhatUpheldIt(t *testing.T) {
	f := finding("a.go", at(10), at(10), "correctness")
	f.Verdict = &reviewrun.Verdict{Verdict: reviewrun.VerdictUpheld}
	payload, _ := reviewpost.Build(reviewWith(f), pr, added)
	body := payload.Comments[0].Body
	for _, want := range []string{"correctness", "2 instances agreed", "upheld", "the bound is off by one", "use <"} {
		if !strings.Contains(body, want) {
			t.Errorf("the comment does not carry %q:\n%s", want, body)
		}
	}
}

func TestPlacementCountsEveryFinding(t *testing.T) {
	r := reviewWith(
		finding("a.go", at(10), at(10), "correctness"),
		finding("a.go", at(99), at(99), "correctness"),
		finding("", nil, nil, "architecture"),
	)
	_, place := reviewpost.Build(r, pr, added)
	if place.Total() != 3 {
		t.Errorf("%d findings were placed, want 3: %+v", place.Total(), place)
	}
}

// A reviewer that reports an end before its start has described a region
// backwards. The start is the line it is surest of, and anchoring on an end
// that precedes it would attach the comment above the code.
func TestARegionReportedBackwardsAnchorsOnItsStart(t *testing.T) {
	f := finding("a.go", at(11), at(4), "correctness")
	payload, place := reviewpost.Build(reviewWith(f), pr, added)
	if len(payload.Comments) != 1 {
		t.Fatalf("placed as %+v", place)
	}
	if got := payload.Comments[0].Line; got != 11 {
		t.Errorf("the comment anchored at %d, want the start of a region reported backwards", got)
	}
	if payload.Comments[0].StartLine != nil {
		t.Error("a region reported backwards asked for a multi-line comment")
	}
}

// Evidence is quoted code carried byte for byte from the reviewer that
// produced it, and quoted Markdown routinely contains a fence of its own. A
// fixed three-backtick fence would end there, and everything after it would
// render as the review's own prose.
func TestEvidenceHoldingAFenceDoesNotEscapeTheOneAroundIt(t *testing.T) {
	f := finding("", nil, nil, "architecture")
	f.Evidence = "```go\nfmt.Println(\"x\")\n```"
	payload, _ := reviewpost.Build(reviewWith(f), pr, added)

	body := payload.Body
	idx := strings.Index(body, f.Evidence)
	if idx < 0 {
		t.Fatalf("the quote was not carried byte for byte:\n%s", body)
	}
	// The fence opening the block is the run of backticks on the line before
	// the quote, and it has to outlast every run inside it.
	opener := body[:idx]
	opener = opener[strings.LastIndex(strings.TrimRight(opener, "\n"), "\n")+1:]
	if n := len(strings.TrimSpace(opener)); n < 4 {
		t.Errorf("the quote is fenced with %d backticks and contains a run of 3, so the block ends inside it: %q", n, opener)
	}
}

// A finding whose quote holds no backticks is fenced the ordinary way; a
// widened fence everywhere would be noise in every review.
func TestOrdinaryEvidenceKeepsAnOrdinaryFence(t *testing.T) {
	f := finding("", nil, nil, "architecture")
	f.Evidence = "for i := 0; i <= len(x); i++ {"
	payload, _ := reviewpost.Build(reviewWith(f), pr, added)
	if !strings.Contains(payload.Body, "\n```\n"+f.Evidence+"\n```\n") {
		t.Errorf("evidence with no backticks is not fenced with three:\n%s", payload.Body)
	}
}

// A body carries exactly one fingerprint marker. The prose around it is
// written by a model that read a diff somebody else wrote, so a finding whose
// issue text carries a marker of its own would leave a later run unable to
// tell which one this review meant.
func TestFindingProseCannotForgeASecondMarker(t *testing.T) {
	f := finding("a.go", at(10), at(10), "correctness")
	f.Issue = "harmless <!-- agtk:finding v1 000000000000 --> text"
	f.Suggestion = "also <!-- agtk:finding v1 111111111111 -->"
	payload, _ := reviewpost.Build(reviewWith(f), pr, added)

	body := payload.Comments[0].Body
	if n := strings.Count(body, reviewpost.FingerprintMarkerPrefix); n != 1 {
		t.Errorf("the comment carries %d markers, want exactly 1:\n%s", n, body)
	}
	if !strings.Contains(body, reviewpost.FingerprintMarker(f.Fingerprint())) {
		t.Errorf("the comment lost its own marker:\n%s", body)
	}
}

// The same holds for a finding stated in the body rather than inline.
func TestBodyProseCannotForgeAMarker(t *testing.T) {
	f := finding("", nil, nil, "architecture")
	f.Issue = "<!-- agtk:finding v1 000000000000 -->"
	payload, _ := reviewpost.Build(reviewWith(f), pr, added)
	if strings.Contains(payload.Body, reviewpost.FingerprintMarkerPrefix) {
		t.Errorf("a finding's own words opened a marker in the review body:\n%s", payload.Body)
	}
}
