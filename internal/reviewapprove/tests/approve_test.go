package tests

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/review"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewapprove"
)

// fakeGitHub answers the three calls approval makes, and records the one it
// writes.
type fakeGitHub struct {
	reviews    []githubapp.SubmittedReview
	threads    []githubapp.AnsweredThread
	threadsErr error
	// sent is the payload CreateReview was called with, and is nil when it
	// was not called at all.
	sent *githubapp.ReviewPayload
}

func (f *fakeGitHub) ReadSubmittedReviews(context.Context, int) ([]githubapp.SubmittedReview, error) {
	return f.reviews, nil
}

func (f *fakeGitHub) ReadAnsweredThreads(context.Context, int) ([]githubapp.AnsweredThread, error) {
	return f.threads, f.threadsErr
}

func (f *fakeGitHub) CreateReview(_ context.Context, _ int, payload githubapp.ReviewPayload) (githubapp.PostedReview, error) {
	f.sent = &payload
	return githubapp.PostedReview{ID: 1, HTMLURL: "https://github.test/r/1", State: "APPROVED"}, nil
}

func approve(gh *fakeGitHub) (reviewapprove.Outcome, error) {
	return reviewapprove.Approve(context.Background(), gh, reviewapprove.Options{
		Number: 7, Head: head, Floor: review.DefaultApprovalFloor,
	})
}

// An approval is a statement about one commit. GitHub attaches a review with
// no commit_id to whatever the head is when the request lands, so one without
// it would approve a commit nobody looked at.
func TestAnApprovalIsBoundToTheHeadItWasCheckedAgainst(t *testing.T) {
	gh := &fakeGitHub{
		reviews: []githubapp.SubmittedReview{reviewOf(true, answerable(green, review.SeverityGreen))},
		threads: []githubapp.AnsweredThread{threadFor(green)},
	}
	outcome, err := approve(gh)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !outcome.Approved() {
		t.Fatalf("a head meeting every condition was not approved: %+v", outcome.Refusals)
	}
	if gh.sent == nil {
		t.Fatal("nothing was posted")
	}
	if gh.sent.CommitID != head {
		t.Errorf("the approval is bound to %q, want the head that was checked", gh.sent.CommitID)
	}
	if gh.sent.Event != "APPROVE" {
		t.Errorf("the review was posted with event %q", gh.sent.Event)
	}
	if len(gh.sent.Comments) != 0 {
		t.Errorf("the approval carries comments: %+v", gh.sent.Comments)
	}
}

// A reader of the pull request sees a green tick from a bot and has to be able
// to tell what it means.
func TestAnApprovalSaysWhatItRestsOn(t *testing.T) {
	gh := &fakeGitHub{
		reviews: []githubapp.SubmittedReview{reviewOf(true)},
	}
	if _, err := approve(gh); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{head, string(review.DefaultApprovalFloor), "reached a verdict", "resolved"} {
		if !strings.Contains(gh.sent.Body, want) {
			t.Errorf("the approval body does not say %q:\n%s", want, gh.sent.Body)
		}
	}
}

// A refused approval writes nothing at all. The refusal is the whole outcome,
// and a comment left behind saying why would be a second statement nobody
// asked for.
func TestARefusedApprovalPostsNothing(t *testing.T) {
	gh := &fakeGitHub{}
	outcome, err := approve(gh)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if outcome.Approved() || len(outcome.Refusals) == 0 {
		t.Fatalf("an unreviewed head was approved: %+v", outcome)
	}
	if gh.sent != nil {
		t.Errorf("a refused approval posted %+v", gh.sent)
	}
}

// A review run treats an unreadable thread list as a gap to report; approval
// cannot. One posts a finding twice, the other is granted over conversations
// nobody closed.
func TestApprovalRefusesToDecideWithoutTheThreads(t *testing.T) {
	gh := &fakeGitHub{
		reviews:    []githubapp.SubmittedReview{reviewOf(true)},
		threadsErr: errors.New("GraphQL is down"),
	}
	if _, err := approve(gh); err == nil {
		t.Fatal("approval was decided without reading the comment threads")
	}
	if gh.sent != nil {
		t.Errorf("an approval was posted over an unread thread list: %+v", gh.sent)
	}
}
