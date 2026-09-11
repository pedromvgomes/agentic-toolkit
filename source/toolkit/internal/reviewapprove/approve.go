package reviewapprove

import (
	"context"
	"fmt"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/review"
)

// eventApprove is the review event that approves a pull request.
//
// Named here and nowhere else in the binary. A guard asserts the literal
// appears in no other package and, more usefully, that no package a model's
// output passes through can reach this one: a string ban holds only until
// somebody spells the event differently, and the import graph holds either
// way.
const eventApprove = "APPROVE"

// GitHub is what approval asks of the API.
//
// An interface rather than the client, because the decision this package makes
// is worth exercising against a pull request nobody has to create, and because
// naming the three calls says exactly how much of GitHub approval touches:
// two reads and one write.
type GitHub interface {
	ReadSubmittedReviews(ctx context.Context, number int) ([]githubapp.SubmittedReview, error)
	ReadAnsweredThreads(ctx context.Context, number int) ([]githubapp.AnsweredThread, error)
	CreateReview(ctx context.Context, number int, payload githubapp.ReviewPayload) (githubapp.PostedReview, error)
}

// Outcome is what an approval attempt came to.
type Outcome struct {
	// Refusals is why the head was not approved, and is empty when it was.
	Refusals []Refusal
	// Posted is the approval GitHub recorded, and is nil when none was.
	Posted *githubapp.PostedReview
}

// Approved reports whether an approval was posted.
func (o Outcome) Approved() bool { return o.Posted != nil }

// Options is what an approval is asked for.
type Options struct {
	// Number is the pull request.
	Number int
	// Head is the commit the approval is bound to. Read from the pull request
	// rather than named by the caller: an approval is a statement about one
	// commit, and one bound to a commit nobody looked at is the failure the
	// binding exists to prevent.
	Head string
	// Floor is the severity at or above which a finding obliges an answer, as
	// the governing manifest sets it.
	Floor review.Severity
}

// Approve reads the pull request, decides whether its head may be approved,
// and posts the approval when it may.
//
// Reading and deciding are one call because the decision is worthless a moment
// later: push access is re-evaluated when the pull request is read rather than
// when a review was submitted, and a thread can be reopened between a check and
// a post. What this returns is what was true when it looked.
func Approve(ctx context.Context, gh GitHub, opts Options) (Outcome, error) {
	reviews, err := gh.ReadSubmittedReviews(ctx, opts.Number)
	if err != nil {
		return Outcome{}, fmt.Errorf("read the reviews on pull request %d: %w", opts.Number, err)
	}
	threads, err := gh.ReadAnsweredThreads(ctx, opts.Number)
	if err != nil {
		// Fatal, where a review run treats the same failure as a gap to report.
		// A run that cannot read threads posts a finding twice; an approval
		// that cannot read them is granted over conversations nobody closed.
		return Outcome{}, fmt.Errorf("read the comment threads on pull request %d: %w", opts.Number, err)
	}

	refusals := Check(Inputs{
		Number:  opts.Number,
		Head:    opts.Head,
		Floor:   opts.Floor,
		Reviews: reviews,
		Threads: threads,
	})
	if len(refusals) > 0 {
		return Outcome{Refusals: refusals}, nil
	}

	posted, err := gh.CreateReview(ctx, opts.Number, githubapp.ReviewPayload{
		CommitID: opts.Head,
		Body:     body(opts),
		Event:    eventApprove,
		Comments: []githubapp.ReviewComment{},
	})
	if err != nil {
		return Outcome{}, fmt.Errorf("approve pull request %d: %w", opts.Number, err)
	}
	return Outcome{Posted: &posted}, nil
}

// body says what the approval rests on.
//
// A reader of the pull request sees a green tick from a bot and has to be able
// to tell what it means. Every clause here is a condition that was checked, and
// there is no sentence for one that was overridden, because none can be.
func body(opts Options) string {
	var b strings.Builder
	b.WriteString("## Approved by `agtk`\n\n")
	fmt.Fprintf(&b, "`%s` carries a review by this installation that reached a verdict. ", opts.Head)
	fmt.Fprintf(&b, "Every finding it reports at or above %s is marked a false positive on its thread by an "+
		"account that can push, and every comment thread on this pull request is resolved.\n\n", opts.Floor)
	b.WriteString("A person typed this. No review run can reach it, and nothing overrides it: " +
		"a finding is cleared by changing the code or by a written statement on its thread, and by nothing else.\n")
	return b.String()
}
