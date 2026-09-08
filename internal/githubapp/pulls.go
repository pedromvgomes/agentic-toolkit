package githubapp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// PullRequest is what a review is posted to, as GitHub reports it.
type PullRequest struct {
	Number int
	// BaseSHA and HeadSHA are the commits the change is measured between.
	// The head is what a posted review is bound to: reviews are per-commit,
	// which is what makes "this commit was reviewed" a fact.
	BaseSHA string
	HeadSHA string
	BaseRef string
	HeadRef string
	State   string
	Title   string
	Draft   bool
}

// ReadPullRequest fetches one pull request's commits and refs.
func (c *Client) ReadPullRequest(ctx context.Context, number int) (PullRequest, error) {
	if number < 1 {
		return PullRequest{}, fmt.Errorf("%d is not a pull request number", number)
	}
	var payload struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		Title  string `json:"title"`
		Draft  bool   `json:"draft"`
		Base   struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"base"`
		Head struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
	}
	path := fmt.Sprintf("/repos/%s/pulls/%d", c.slug, number)
	if err := c.call(ctx, http.MethodGet, path, nil, &payload); err != nil {
		var apiErr *Error
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return PullRequest{}, fmt.Errorf("%s has no pull request %d", c.slug, number)
		}
		return PullRequest{}, err
	}
	if payload.Head.SHA == "" || payload.Base.SHA == "" {
		return PullRequest{}, fmt.Errorf("GitHub reported pull request %d with no base or head commit", number)
	}
	return PullRequest{
		Number:  payload.Number,
		BaseSHA: payload.Base.SHA,
		HeadSHA: payload.Head.SHA,
		BaseRef: payload.Base.Ref,
		HeadRef: payload.Head.Ref,
		State:   payload.State,
		Title:   payload.Title,
		Draft:   payload.Draft,
	}, nil
}

// EventComment is the only event a review run posts. Approval is a separate
// act, and no code path from a review reaches it — see ADR 0006.
const EventComment = "COMMENT"

// SideRight addresses the head of the change. A comment on the base side
// would be a remark about code the pull request does not propose.
const SideRight = "RIGHT"

// ReviewPayload is the one API call a review run makes.
type ReviewPayload struct {
	// CommitID binds the review to the head it was made against. Without it
	// GitHub attaches the review to whatever the head is when the request
	// lands, so a push mid-review would produce a review claiming to describe
	// a commit nobody looked at.
	CommitID string `json:"commit_id"`
	Body     string `json:"body"`
	Event    string `json:"event"`
	// Comments is never omitted: a review with no inline comments sends an
	// empty list rather than a null, which GitHub reads as a malformed field.
	Comments []ReviewComment `json:"comments"`
}

// ReviewComment is one inline comment, addressed the way the current API
// addresses them: by line in the head's diff rather than by position in it.
type ReviewComment struct {
	Path string `json:"path"`
	// Line is the last line of the region, in the head's numbering.
	Line int    `json:"line"`
	Side string `json:"side"`
	// StartLine and StartSide are set only for a multi-line comment. GitHub
	// refuses a request whose start line equals its line.
	StartLine *int    `json:"start_line,omitempty"`
	StartSide *string `json:"start_side,omitempty"`
	Body      string  `json:"body"`
}

// PostedReview is what GitHub made of a posted review.
type PostedReview struct {
	ID      int64  `json:"id"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
}

// CreateReview posts one review to a pull request.
//
// One call, and it is the only one a review run makes that writes anything.
// GitHub rejects the whole batch with a 422 when a single comment names a line
// outside the head's diff, which is why positioning happens before this is
// reached rather than being discovered here.
func (c *Client) CreateReview(ctx context.Context, number int, payload ReviewPayload) (PostedReview, error) {
	if payload.Comments == nil {
		payload.Comments = []ReviewComment{}
	}
	var posted PostedReview
	path := fmt.Sprintf("/repos/%s/pulls/%d/reviews", c.slug, number)
	if err := c.call(ctx, http.MethodPost, path, payload, &posted); err != nil {
		return PostedReview{}, err
	}
	return posted, nil
}
