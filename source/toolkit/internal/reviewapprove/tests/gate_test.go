package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/review"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewapprove"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun"
)

// head is the commit every pull request here is at.
const head = "0123456789abcdef0123456789abcdef01234567"

// fingerprints used across the cases. Twelve hex characters, the width a
// fingerprint marker carries.
const (
	red   = "aaaaaaaaaaaa"
	amber = "bbbbbbbbbbbb"
	green = "cccccccccccc"
)

// reviewOf builds a review this installation posted against head, carrying a
// marker for the findings named.
func reviewOf(complete bool, findings ...reviewrun.MarkedFinding) githubapp.SubmittedReview {
	marker := reviewrun.ReviewMarker{Head: head, Complete: complete, Findings: findings}
	return githubapp.SubmittedReview{
		CommitSHA: head,
		Body:      "## Review by `agtk`\n\n" + marker.Render() + "\n",
		ByViewer:  true,
	}
}

// answerable is a finding the review gave a thread to.
func answerable(fingerprint string, severity review.Severity) reviewrun.MarkedFinding {
	return reviewrun.MarkedFinding{Fingerprint: fingerprint, Severity: severity, Answerable: true}
}

// threadFor builds a resolved thread this installation opened for one finding.
func threadFor(fingerprint string, replies ...githubapp.ThreadReply) githubapp.AnsweredThread {
	return githubapp.AnsweredThread{
		ReviewThread: githubapp.ReviewThread{
			Path:     "a.go",
			Resolved: true,
			Body:     "**RED — correctness**\n\noff by one\n\n" + reviewrun.FingerprintMarker(fingerprint) + "\n",
			ByViewer: true,
		},
		Replies: replies,
	}
}

// marking is a reply that clears a finding, from an account that can push.
func marking(reason string) githubapp.ThreadReply {
	return githubapp.ThreadReply{
		Body:              reviewapprove.Marking + ": " + reason,
		AuthorAssociation: githubapp.AssociationOwner,
	}
}

// gate runs the check over one pull request at the default floor.
func gate(reviews []githubapp.SubmittedReview, threads []githubapp.AnsweredThread) []reviewapprove.Refusal {
	return reviewapprove.Check(reviewapprove.Inputs{
		Number: 7, Head: head, Floor: review.DefaultApprovalFloor,
		Reviews: reviews, Threads: threads,
	})
}

// missing renders every refusal, for a test that asks what was said.
func missing(refusals []reviewapprove.Refusal) string {
	var b strings.Builder
	for _, r := range refusals {
		b.WriteString(r.Missing + " | " + r.Remedy + "\n")
	}
	return b.String()
}

// All four conditions hold, so the head may be approved. Everything else here
// is one of them failing.
func TestAReviewedHeadWithEveryFindingAnsweredIsApproved(t *testing.T) {
	refusals := gate(
		[]githubapp.SubmittedReview{reviewOf(true,
			answerable(red, review.SeverityRed),
			answerable(green, review.SeverityGreen),
		)},
		[]githubapp.AnsweredThread{
			threadFor(red, marking("the caller guarantees the bound")),
			threadFor(green),
		},
	)
	if len(refusals) != 0 {
		t.Errorf("a head that meets every condition was refused:\n%s", missing(refusals))
	}
}

// A head nobody reviewed has no findings to weigh and no verdict to read, so
// nothing else is knowable — and listing the threads it happens to carry
// alongside that would suggest they were the obstacle.
func TestAnUnreviewedHeadIsRefusedWithTheCommandThatWouldReviewIt(t *testing.T) {
	refusals := gate(nil, []githubapp.AnsweredThread{threadFor(red)})
	if len(refusals) != 1 {
		t.Fatalf("an unreviewed head produced %d refusals:\n%s", len(refusals), missing(refusals))
	}
	if !strings.Contains(refusals[0].Missing, head) {
		t.Errorf("the refusal does not name the commit: %q", refusals[0].Missing)
	}
	if !strings.Contains(refusals[0].Remedy, "code-review run --pr 7") {
		t.Errorf("the refusal does not name what would answer it: %q", refusals[0].Remedy)
	}
}

// Anyone who can review a pull request can type the characters that open a
// review marker. One claiming a clean review of the current head, believed,
// would be an approval somebody granted themselves by commenting.
func TestAMarkerInSomebodyElsesReviewIsNotBelieved(t *testing.T) {
	forged := reviewOf(true)
	forged.ByViewer = false
	if refusals := gate([]githubapp.SubmittedReview{forged}, nil); len(refusals) != 1 {
		t.Errorf("a marker written by another account was read as this installation's:\n%s", missing(refusals))
	}
}

// A review is bound to a commit, which is what makes "this commit was
// reviewed" a fact rather than "this pull request was".
func TestAReviewOfAnEarlierHeadDoesNotCoverThisOne(t *testing.T) {
	stale := reviewOf(true)
	stale.CommitSHA = strings.Repeat("9", 40)
	if refusals := gate([]githubapp.SubmittedReview{stale}, nil); len(refusals) != 1 {
		t.Errorf("a review of another commit was accepted for this head:\n%s", missing(refusals))
	}
}

// A body can be edited after it is posted. A marker describing another commit
// is not a review of this one, whatever the review is attached to.
func TestAMarkerNamingAnotherCommitIsNotAReviewOfThisHead(t *testing.T) {
	edited := reviewOf(true)
	edited.Body = strings.Replace(edited.Body, "head="+head, "head="+strings.Repeat("9", 40), 1)
	if refusals := gate([]githubapp.SubmittedReview{edited}, nil); len(refusals) != 1 {
		t.Errorf("a marker naming another commit was accepted:\n%s", missing(refusals))
	}
}

// A run that could not reach a verdict found less than it would have. Reading
// that as a clean review is the one thing approval must never do.
func TestAReviewThatReachedNoVerdictDoesNotUnblockApproval(t *testing.T) {
	refusals := gate([]githubapp.SubmittedReview{reviewOf(false)}, nil)
	if len(refusals) != 1 || !strings.Contains(refusals[0].Missing, "did not reach a verdict") {
		t.Errorf("a review with no verdict was accepted:\n%s", missing(refusals))
	}
}

// A defect is cleared by changing the code or by a written statement on its
// thread, and by nothing else. Resolution says the conversation is finished,
// which is a different claim.
func TestResolvingAThreadDoesNotClearTheFindingOnIt(t *testing.T) {
	refusals := gate(
		[]githubapp.SubmittedReview{reviewOf(true, answerable(red, review.SeverityRed))},
		[]githubapp.AnsweredThread{threadFor(red)},
	)
	if len(refusals) != 1 {
		t.Fatalf("a resolved thread with no marking produced %d refusals:\n%s", len(refusals), missing(refusals))
	}
	if !strings.Contains(refusals[0].Missing, red) {
		t.Errorf("the refusal does not name the finding: %q", refusals[0].Missing)
	}
	if !strings.Contains(refusals[0].Remedy, reviewapprove.Marking) {
		t.Errorf("the refusal does not say what to type: %q", refusals[0].Remedy)
	}
}

// The author of a change is the party a review does not trust. A finding its
// own author could dismiss is one an injected instruction can dismiss too.
func TestAMarkingFromAnAccountWithoutWriteAccessClearsNothing(t *testing.T) {
	for _, association := range []string{"CONTRIBUTOR", "FIRST_TIME_CONTRIBUTOR", "NONE", "MANNEQUIN", ""} {
		reply := marking("I disagree")
		reply.AuthorAssociation = association
		refusals := gate(
			[]githubapp.SubmittedReview{reviewOf(true, answerable(red, review.SeverityRed))},
			[]githubapp.AnsweredThread{threadFor(red, reply)},
		)
		if len(refusals) != 1 {
			t.Errorf("%s cleared a finding:\n%s", association, missing(refusals))
		}
	}
}

// Every association that carries write access clears a finding, because each
// one is somebody GitHub lets push.
func TestAMarkingFromAnAccountThatCanPushClearsTheFinding(t *testing.T) {
	for _, association := range []string{
		githubapp.AssociationOwner, githubapp.AssociationMember, githubapp.AssociationCollaborator,
	} {
		reply := marking("the bound is guaranteed upstream")
		reply.AuthorAssociation = association
		refusals := gate(
			[]githubapp.SubmittedReview{reviewOf(true, answerable(red, review.SeverityRed))},
			[]githubapp.AnsweredThread{threadFor(red, reply)},
		)
		if len(refusals) != 0 {
			t.Errorf("%s could not clear a finding:\n%s", association, missing(refusals))
		}
	}
}

// A marking with no reason is the click this asks for a sentence instead of.
func TestAMarkingWithNoReasonClearsNothing(t *testing.T) {
	bare := githubapp.ThreadReply{Body: reviewapprove.Marking, AuthorAssociation: githubapp.AssociationOwner}
	refusals := gate(
		[]githubapp.SubmittedReview{reviewOf(true, answerable(red, review.SeverityRed))},
		[]githubapp.AnsweredThread{threadFor(red, bare)},
	)
	if len(refusals) != 1 {
		t.Errorf("a marking carrying no reason cleared a finding:\n%s", missing(refusals))
	}
}

// The marking opens a line, so a reply quoting the syntax while arguing
// against it does not clear anything.
func TestAReplyThatMerelyMentionsTheMarkingClearsNothing(t *testing.T) {
	quoting := githubapp.ThreadReply{
		Body:              "I do not think `" + reviewapprove.Marking + ": whatever` applies here, this is real.",
		AuthorAssociation: githubapp.AssociationOwner,
	}
	refusals := gate(
		[]githubapp.SubmittedReview{reviewOf(true, answerable(red, review.SeverityRed))},
		[]githubapp.AnsweredThread{threadFor(red, quoting)},
	)
	if len(refusals) != 1 {
		t.Errorf("a reply that only mentions the marking cleared a finding:\n%s", missing(refusals))
	}
}

// AMBER is the default floor, so RED and AMBER both oblige an answer and GREEN
// obliges only that its thread be resolved.
func TestTheFloorDecidesWhichFindingsObligeAnAnswer(t *testing.T) {
	reviews := []githubapp.SubmittedReview{reviewOf(true,
		answerable(red, review.SeverityRed),
		answerable(amber, review.SeverityAmber),
		answerable(green, review.SeverityGreen),
	)}
	threads := []githubapp.AnsweredThread{threadFor(red), threadFor(amber), threadFor(green)}

	atAmber := gate(reviews, threads)
	if len(atAmber) != 2 {
		t.Errorf("at the AMBER floor %d findings oblige an answer, want RED and AMBER:\n%s", len(atAmber), missing(atAmber))
	}
	atRed := reviewapprove.Check(reviewapprove.Inputs{
		Number: 7, Head: head, Floor: review.SeverityRed, Reviews: reviews, Threads: threads,
	})
	if len(atRed) != 1 || !strings.Contains(atRed[0].Missing, red) {
		t.Errorf("at the RED floor the obliged set is not RED alone:\n%s", missing(atRed))
	}
}

// A gate with no remedy is a deadlock rather than a control, and a finding
// about code the change does not touch is not a finding about the change.
func TestAFindingWithNoThreadToAnswerOnBlocksNothing(t *testing.T) {
	refusals := gate(
		[]githubapp.SubmittedReview{reviewOf(true,
			reviewrun.MarkedFinding{Fingerprint: red, Severity: review.SeverityRed},
		)},
		nil,
	)
	if len(refusals) != 0 {
		t.Errorf("an unanswerable finding blocked approval:\n%s", missing(refusals))
	}
}

// Prompt injection is the one exception, and the deadlock is the point: the
// material addresses the reviewer, there is nothing to reply to, and the
// remedy is to change the code.
func TestAnUnattachedInjectionFindingRefusesWithNoRemedy(t *testing.T) {
	refusals := gate(
		[]githubapp.SubmittedReview{reviewOf(true,
			reviewrun.MarkedFinding{Fingerprint: red, Severity: review.SeverityRed, Injected: true},
		)},
		nil,
	)
	if len(refusals) != 1 {
		t.Fatalf("an unattached injection finding produced %d refusals:\n%s", len(refusals), missing(refusals))
	}
	if refusals[0].Remedy != "" {
		t.Errorf("the deadlock offers a way past it: %q", refusals[0].Remedy)
	}
}

// The review says it gave this finding a comment and the pull request carries
// none, so the request that would have opened the thread did not land. Nobody
// can answer a thread that is not there.
func TestAnAnswerableFindingWithNoThreadOnThePullRequestIsRefused(t *testing.T) {
	refusals := gate(
		[]githubapp.SubmittedReview{reviewOf(true, answerable(red, review.SeverityRed))},
		nil,
	)
	if len(refusals) != 1 || !strings.Contains(refusals[0].Missing, "carries no thread") {
		t.Fatalf("a finding whose comment never landed was not reported:\n%s", missing(refusals))
	}
	if !strings.Contains(refusals[0].Remedy, "--force") {
		t.Errorf("the remedy does not re-post the comment: %q", refusals[0].Remedy)
	}
}

// A review that reached no verdict does not suppress the next run, so the
// remedy for one is a plain run. Naming --force here sends a person reaching
// for an override nothing is stopping them without, to get past a review that
// examined nothing.
func TestTheRemedyForANoVerdictReviewDoesNotReachForForce(t *testing.T) {
	refusals := gate([]githubapp.SubmittedReview{reviewOf(false)}, nil)
	if len(refusals) != 1 || !strings.Contains(refusals[0].Missing, "did not reach a verdict") {
		t.Fatalf("a review with no verdict was not refused:\n%s", missing(refusals))
	}
	if strings.Contains(refusals[0].Remedy, "--force") {
		t.Errorf("the remedy reaches for --force to get past a review that found nothing: %q",
			refusals[0].Remedy)
	}
	if !strings.Contains(refusals[0].Remedy, "code-review run --pr") {
		t.Errorf("the remedy does not re-review the head: %q", refusals[0].Remedy)
	}
}

// The same refusal on a complete review keeps --force, because a head whose
// newest review reached a verdict is the one case a plain run declines to
// pass.
func TestAFindingWithNoThreadOnACompleteReviewStillNeedsForce(t *testing.T) {
	refusals := gate(
		[]githubapp.SubmittedReview{reviewOf(true, answerable(red, review.SeverityRed))},
		nil,
	)
	if len(refusals) != 1 {
		t.Fatalf("want one refusal, got:\n%s", missing(refusals))
	}
	if !strings.Contains(refusals[0].Remedy, "--force") {
		t.Errorf("a complete review's re-post remedy dropped --force: %q", refusals[0].Remedy)
	}
}

// A fingerprint marker in a comment somebody else wrote is a claim about
// identity from an author who does not hold it.
func TestAThreadSomebodyElseOpenedCannotCarryAFindingsIdentity(t *testing.T) {
	forged := threadFor(red, marking("not a defect"))
	forged.ByViewer = false
	refusals := gate(
		[]githubapp.SubmittedReview{reviewOf(true, answerable(red, review.SeverityRed))},
		[]githubapp.AnsweredThread{forged},
	)
	if len(refusals) != 1 || !strings.Contains(refusals[0].Missing, "carries no thread") {
		t.Errorf("a thread opened by another account cleared a finding:\n%s", missing(refusals))
	}
}

// An open conversation about this change is an open conversation whoever
// started it, so every thread counts and not only the ones agtk opened.
func TestAnUnresolvedThreadRefusesWhoeverOpenedIt(t *testing.T) {
	human := githubapp.AnsweredThread{
		ReviewThread: githubapp.ReviewThread{Path: "b.go", Resolved: false},
	}
	refusals := gate([]githubapp.SubmittedReview{reviewOf(true)}, []githubapp.AnsweredThread{human})
	if len(refusals) != 1 || !strings.Contains(refusals[0].Missing, "b.go") {
		t.Errorf("an unresolved thread nobody from agtk opened was ignored:\n%s", missing(refusals))
	}
}

// A marking past the cut is a marking approval did not read, and refusing on
// what it cannot see is the safe half of that.
func TestAThreadWithMoreRepliesThanWereReadIsRefusedAndSaysSo(t *testing.T) {
	truncated := threadFor(red)
	truncated.Truncated = true
	refusals := gate(
		[]githubapp.SubmittedReview{reviewOf(true, answerable(red, review.SeverityRed))},
		[]githubapp.AnsweredThread{truncated},
	)
	if len(refusals) != 1 || !strings.Contains(refusals[0].Missing, "too many to read whole") {
		t.Errorf("a thread read only in part was not reported as such:\n%s", missing(refusals))
	}
}

// Somebody who has to run the command five times to discover five refusals
// learns the gate is an obstacle; somebody told all five once can answer them.
func TestEveryUnmetConditionIsReportedAtOnce(t *testing.T) {
	refusals := gate(
		[]githubapp.SubmittedReview{reviewOf(false,
			answerable(red, review.SeverityRed),
			answerable(amber, review.SeverityAmber),
		)},
		[]githubapp.AnsweredThread{
			threadFor(red),
			{ReviewThread: githubapp.ReviewThread{Path: "b.go"}},
		},
	)
	// No verdict, RED unanswered, AMBER with no thread, and an open thread.
	if len(refusals) != 4 {
		t.Errorf("%d of the four unmet conditions were reported:\n%s", len(refusals), missing(refusals))
	}
}

// A head re-reviewed with --force carries two reviews, and the later one is
// what the pull request now says.
func TestTheNewestReviewOfTheHeadIsWhatIsRead(t *testing.T) {
	refusals := gate(
		[]githubapp.SubmittedReview{
			reviewOf(true, answerable(red, review.SeverityRed)),
			reviewOf(true),
		},
		nil,
	)
	if len(refusals) != 0 {
		t.Errorf("the earlier review of this head decided the gate:\n%s", missing(refusals))
	}
}
