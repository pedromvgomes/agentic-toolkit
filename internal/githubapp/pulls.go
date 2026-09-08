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

// SubjectFile addresses a comment at a whole file rather than at a line in it.
const SubjectFile = "file"

// FileComment is one comment against a whole file in a pull request's diff.
//
// Its own request, and never part of a review. `subject_type` is not a field
// on a review's draft comments — GitHub answers 422 "Field is not defined on
// DraftPullRequestReviewComment" — so a finding that carries no line, or one
// whose line the diff does not add, is posted after the review rather than
// inside it. That is the cost of giving such a finding a thread somebody can
// answer on, and it is why a review run's "exactly one API call" holds for the
// review proper and not for these.
type FileComment struct {
	// CommitID binds the comment to the head it describes, the way a review
	// is bound to the commit it was made against.
	CommitID    string `json:"commit_id"`
	Path        string `json:"path"`
	SubjectType string `json:"subject_type"`
	Body        string `json:"body"`
}

// PostedComment is what GitHub made of a posted comment.
type PostedComment struct {
	ID      int64  `json:"id"`
	HTMLURL string `json:"html_url"`
}

// CreateFileComment posts one comment against a whole file.
//
// GitHub refuses a path the pull request's diff does not touch with a 422
// naming pull_request_review_thread.path, which is why a finding on such a
// path is recognised as unattachable before this is reached rather than being
// discovered here. One that fails anyway leaves its finding stated in the
// review body, which is reported: unlike a review's inline comments, these
// fail one at a time and cost nothing but themselves.
func (c *Client) CreateFileComment(ctx context.Context, number int, comment FileComment) (PostedComment, error) {
	if comment.SubjectType == "" {
		comment.SubjectType = SubjectFile
	}
	var posted PostedComment
	path := fmt.Sprintf("/repos/%s/pulls/%d/comments", c.slug, number)
	if err := c.call(ctx, http.MethodPost, path, comment, &posted); err != nil {
		return PostedComment{}, err
	}
	return posted, nil
}
