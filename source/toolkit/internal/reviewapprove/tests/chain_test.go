package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/review"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewapprove"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun"
)

// earlier is a commit the pull request was at before head, which a full
// review read.
const earlier = "fedcba9876543210fedcba9876543210fedcba98"

// reviewAt builds a review this installation posted against one commit,
// reading on from since when since is not empty.
func reviewAt(at, since string, complete bool, findings ...reviewrun.MarkedFinding) githubapp.SubmittedReview {
	marker := reviewrun.ReviewMarker{Head: at, Since: since, Complete: complete, Findings: findings}
	return githubapp.SubmittedReview{
		CommitSHA: at,
		Body:      "## Review by `agtk`\n\n" + marker.Render() + "\n",
		ByViewer:  true,
	}
}

// answer is a reply from an account that can push that says what was done.
func answer(body string) githubapp.ThreadReply {
	return githubapp.ThreadReply{Body: body, AuthorAssociation: githubapp.AssociationOwner}
}

// open is a thread left unresolved.
func open(thread githubapp.AnsweredThread) githubapp.AnsweredThread {
	thread.Resolved = false
	return thread
}

// A review that read only what changed since an earlier one speaks for the
// whole change together with it. The chain walks back to the review that read
// everything, newest first.
func TestTheChainWalksBackToAReviewOfTheWholeChange(t *testing.T) {
	chain, intact := reviewapprove.ReviewChain([]githubapp.SubmittedReview{
		reviewAt(earlier, "", true),
		reviewAt(head, earlier, true),
	}, head)
	if !intact {
		t.Fatal("a delta review reading on from a complete full review is reported broken")
	}
	if len(chain) != 2 || chain[0].Head != head || chain[1].Head != earlier {
		t.Fatalf("chain = %+v", chain)
	}
}

// Each ways a link can fail leaves part of the change read by nobody.
func TestABrokenChainRefusesAndAsksForAFullReview(t *testing.T) {
	cases := map[string][]githubapp.SubmittedReview{
		"the earlier review is missing": {
			reviewAt(head, earlier, true),
		},
		"the earlier review reached no verdict": {
			reviewAt(earlier, "", false),
			reviewAt(head, earlier, true),
		},
		"the earlier review was not posted by this installation": {
			func() githubapp.SubmittedReview {
				r := reviewAt(earlier, "", true)
				r.ByViewer = false
				return r
			}(),
			reviewAt(head, earlier, true),
		},
	}
	for name, reviews := range cases {
		t.Run(name, func(t *testing.T) {
			refusals := gate(reviews, nil)
			said := missing(refusals)
			if !strings.Contains(said, "read by nobody") || !strings.Contains(said, "--full") {
				t.Fatalf("a broken chain is not refused with a full review as the remedy:\n%s", said)
			}
		})
	}
}

// A finding an earlier review made, which the delta review did not repeat, is
// cleared only by a statement on its thread: an answer from somebody who can
// push, and the thread resolved — or a false-positive marking.
func TestACarriedFindingIsClearedByAnAnswerAndResolution(t *testing.T) {
	reviews := []githubapp.SubmittedReview{
		reviewAt(earlier, "", true, answerable(amber, review.SeverityAmber)),
		reviewAt(head, earlier, true),
	}
	cases := []struct {
		name    string
		thread  githubapp.AnsweredThread
		refused bool
	}{
		{"answered and resolved", threadFor(amber, answer("fixed in abc123")), false},
		{"marked a false positive", open(threadFor(amber, marking("intended"))), false},
		{"answered and left open", open(threadFor(amber, answer("fixed in abc123"))), true},
		{"resolved with no answer", threadFor(amber), true},
		{"answered by somebody who cannot push", threadFor(amber, githubapp.ThreadReply{
			Body: "fixed", AuthorAssociation: "CONTRIBUTOR",
		}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			said := missing(gate(reviews, []githubapp.AnsweredThread{tc.thread}))
			carried := strings.Contains(said, "carried from the review of "+earlier)
			if carried != tc.refused {
				t.Fatalf("refused = %v, want %v:\n%s", carried, tc.refused, said)
			}
		})
	}
}

// A finding the delta review repeats is the head's own, and is held to the
// head's rule rather than the carried one: an answer and a resolution do not
// clear a defect a review of this head still reports.
func TestAFindingTheDeltaRepeatsIsHeldToTheHeadsRule(t *testing.T) {
	reviews := []githubapp.SubmittedReview{
		reviewAt(earlier, "", true, answerable(amber, review.SeverityAmber)),
		reviewAt(head, earlier, true, answerable(amber, review.SeverityAmber)),
	}
	said := missing(gate(reviews, []githubapp.AnsweredThread{threadFor(amber, answer("fixed in abc123"))}))
	if strings.Contains(said, "carried from") {
		t.Errorf("a finding the head's review repeats is treated as carried:\n%s", said)
	}
	if !strings.Contains(said, "neither fixed nor marked a false positive") {
		t.Errorf("a finding the head's review still reports is cleared by an answer:\n%s", said)
	}
}

// A carried remark obliges nothing, as it did when it was reported.
func TestACarriedFindingBelowTheFloorBlocksNothing(t *testing.T) {
	reviews := []githubapp.SubmittedReview{
		reviewAt(earlier, "", true, answerable(green, review.SeverityGreen)),
		reviewAt(head, earlier, true),
	}
	if refusals := gate(reviews, []githubapp.AnsweredThread{threadFor(green)}); len(refusals) != 0 {
		t.Fatalf("a carried GREEN refused approval:\n%s", missing(refusals))
	}
}

// A carried prompt-injection finding with no thread is a deadlock no delta
// review can lift, because none reads the code it names unless that changed.
func TestACarriedDeadlockAsksForAFullReview(t *testing.T) {
	stuck := reviewrun.MarkedFinding{Fingerprint: red, Severity: review.SeverityRed, Injected: true}
	reviews := []githubapp.SubmittedReview{
		reviewAt(earlier, "", true, stuck),
		reviewAt(head, earlier, true),
	}
	said := missing(gate(reviews, nil))
	if !strings.Contains(said, "addressed at the reviewer") || !strings.Contains(said, "--full") {
		t.Fatalf("a carried deadlock is not refused with a full review as the remedy:\n%s", said)
	}
}

// A review that read the whole change is read exactly as before: the chain is
// the review itself.
func TestAFullReviewHasNoCarriedFindings(t *testing.T) {
	reviews := []githubapp.SubmittedReview{
		reviewAt(earlier, "", true, answerable(amber, review.SeverityAmber)),
		reviewAt(head, "", true),
	}
	if refusals := gate(reviews, nil); len(refusals) != 0 {
		t.Fatalf("a full review of head was held to an earlier review's findings:\n%s", missing(refusals))
	}
}
