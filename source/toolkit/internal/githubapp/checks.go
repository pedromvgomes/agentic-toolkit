package githubapp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// CheckRun is one check run on a commit, as GitHub's checks API reports it.
type CheckRun struct {
	Name string
	// Status is "queued", "in_progress", "completed", "waiting", "requested"
	// or "pending". Only a completed run carries a conclusion.
	Status string
	// Conclusion is "success", "failure", "neutral", "cancelled", "skipped",
	// "timed_out", "action_required" or "stale", and empty while the run has
	// not completed.
	Conclusion string
	// HTMLURL is the run's page on GitHub; DetailsURL is where the
	// integration that ran it sends a reader, and may be the same page.
	HTMLURL    string
	DetailsURL string
	// App is the slug of the GitHub App that created the run, and empty when
	// GitHub reports none.
	App string
}

// CommitStatus is one context's latest status on a commit.
type CommitStatus struct {
	Context string
	// State is "pending", "success", "failure" or "error".
	State       string
	Description string
	TargetURL   string
}

// CombinedStatus is a commit's statuses, one per context, and the state
// GitHub combines them into.
type CombinedStatus struct {
	// State is "pending", "success" or "failure". GitHub reports "pending"
	// for a commit with no statuses at all.
	State    string
	Statuses []CommitStatus
}

// IssueComment is one comment on an issue or a pull request's conversation.
type IssueComment struct {
	ID int64
	// Author is the login of the account that wrote the comment, and empty
	// when that account has been deleted.
	Author    string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// checksPerPage bounds one page of a check run read.
//
// Every run arrives with its output, whose summary and text GitHub accepts at
// 65535 characters each. A page that overruns maxBody arrives as a JSON parse
// failure, which refuses the read, so the page is kept small enough that the
// ordinary case never meets the cap.
const checksPerPage = 25

// statusesPerPage bounds one page of a combined status read. A status is a
// handful of short fields, so the page is GitHub's largest.
const statusesPerPage = 100

// commentsPerIssuePage bounds one page of an issue comment read, for the
// reason issuesPerPage bounds an issue page: a comment body may be 65536
// characters.
const commentsPerIssuePage = 25

// validSHA reports whether sha is a full commit hash: 40 hexadecimal digits
// for SHA-1, 64 for SHA-256. An abbreviation or a branch name is refused:
// the commits endpoints resolve either when the request lands, so its answer
// can describe a commit other than the one the caller read.
func validSHA(sha string) bool {
	if len(sha) != 40 && len(sha) != 64 {
		return false
	}
	for _, r := range sha {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// ReadCheckRuns reads every check run on the commit sha, the latest run of
// each name.
//
// The whole list or an error. GitHub reports how many runs there are, and a
// read that collects a different number — a run created or removed between
// pages, or pages that ended early — is refused rather than returned: a list
// missing its one failing run reads as a green commit.
func (c *Client) ReadCheckRuns(ctx context.Context, sha string) ([]CheckRun, error) {
	if !validSHA(sha) {
		return nil, fmt.Errorf("%q is not a full commit SHA", sha)
	}
	what := fmt.Sprintf("the check runs on %s@%s", c.slug, sha)
	path := fmt.Sprintf("/repos/%s/commits/%s/check-runs", c.slug, sha)
	query := url.Values{"filter": {"latest"}, "per_page": {fmt.Sprint(checksPerPage)}}
	out := []CheckRun{}
	total := -1
	for page := 1; ; page++ {
		if page > maxPages {
			return nil, fmt.Errorf("%s did not end after %d pages of %d", what, maxPages, checksPerPage)
		}
		query.Set("page", fmt.Sprint(page))
		var payload struct {
			TotalCount *int `json:"total_count"`
			CheckRuns  []struct {
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				HTMLURL    string `json:"html_url"`
				DetailsURL string `json:"details_url"`
				App        *struct {
					Slug string `json:"slug"`
				} `json:"app"`
			} `json:"check_runs"`
		}
		if err := c.call(ctx, http.MethodGet, path+"?"+query.Encode(), nil, &payload); err != nil {
			return nil, classifyRead(err)
		}
		if payload.TotalCount == nil {
			return nil, fmt.Errorf("GitHub reported %s with no total count", what)
		}
		if total >= 0 && *payload.TotalCount != total { // [lydite:exclude_from_mutation][total is -1 only before the first page, and a read whose count is 0 returns or is refused on that page, so total is never 0 here and >= and > agree]
			return nil, fmt.Errorf("%s changed from %d to %d runs while they were read", what, total, *payload.TotalCount)
		}
		total = *payload.TotalCount
		for _, run := range payload.CheckRuns {
			if run.Name == "" || run.Status == "" {
				return nil, fmt.Errorf("GitHub reported a run in %s with no name or status", what)
			}
			read := CheckRun{
				Name:       run.Name,
				Status:     run.Status,
				Conclusion: run.Conclusion,
				HTMLURL:    run.HTMLURL,
				DetailsURL: run.DetailsURL,
			}
			if run.App != nil {
				read.App = run.App.Slug
			}
			out = append(out, read)
		}
		if err := countSettled(what, len(out), total, len(payload.CheckRuns)); err != nil {
			return nil, err
		}
		if len(out) == total {
			return out, nil
		}
	}
}

// ReadCommitStatuses reads the combined status of the commit sha: every
// context's latest status, and the state GitHub combines them into.
//
// The whole list or an error, by the count GitHub reports, for the reason
// ReadCheckRuns refuses a short list.
func (c *Client) ReadCommitStatuses(ctx context.Context, sha string) (CombinedStatus, error) {
	if !validSHA(sha) {
		return CombinedStatus{}, fmt.Errorf("%q is not a full commit SHA", sha)
	}
	what := fmt.Sprintf("the commit statuses on %s@%s", c.slug, sha)
	path := fmt.Sprintf("/repos/%s/commits/%s/status", c.slug, sha)
	query := url.Values{"per_page": {fmt.Sprint(statusesPerPage)}}
	out := CombinedStatus{Statuses: []CommitStatus{}}
	total := -1
	for page := 1; ; page++ {
		if page > maxPages {
			return CombinedStatus{}, fmt.Errorf("%s did not end after %d pages of %d", what, maxPages, statusesPerPage)
		}
		query.Set("page", fmt.Sprint(page))
		var payload struct {
			State      string `json:"state"`
			TotalCount *int   `json:"total_count"`
			Statuses   []struct {
				Context     string `json:"context"`
				State       string `json:"state"`
				Description string `json:"description"`
				TargetURL   string `json:"target_url"`
			} `json:"statuses"`
		}
		if err := c.call(ctx, http.MethodGet, path+"?"+query.Encode(), nil, &payload); err != nil {
			return CombinedStatus{}, classifyRead(err)
		}
		if payload.State == "" || payload.TotalCount == nil {
			return CombinedStatus{}, fmt.Errorf("GitHub reported %s with no state or total count", what)
		}
		if total >= 0 && (*payload.TotalCount != total || payload.State != out.State) { // [lydite:exclude_from_mutation][total is -1 only before the first page, and a read whose count is 0 returns or is refused on that page, so total is never 0 here and >= and > agree]
			return CombinedStatus{}, fmt.Errorf("%s changed while they were read", what)
		}
		total, out.State = *payload.TotalCount, payload.State
		for _, status := range payload.Statuses {
			if status.Context == "" || status.State == "" {
				return CombinedStatus{}, fmt.Errorf("GitHub reported a status in %s with no context or state", what)
			}
			out.Statuses = append(out.Statuses, CommitStatus{
				Context:     status.Context,
				State:       status.State,
				Description: status.Description,
				TargetURL:   status.TargetURL,
			})
		}
		if err := countSettled(what, len(out.Statuses), total, len(payload.Statuses)); err != nil {
			return CombinedStatus{}, err
		}
		if len(out.Statuses) == total {
			return out, nil
		}
	}
}

// countSettled refuses a counted read that has collected more items than
// GitHub reported, or that met an empty page before collecting them all.
// Either is a list that is not the one the count describes.
func countSettled(what string, collected, total, onPage int) error {
	switch {
	case collected > total:
		return fmt.Errorf("%s returned %d items where GitHub reported %d", what, collected, total)
	case collected < total && onPage == 0:
		return fmt.Errorf("%s ended after %d items where GitHub reported %d", what, collected, total)
	}
	return nil
}

// ReadIssueComments reads every comment on the numbered issue or pull
// request's conversation, oldest first.
//
// The whole list or an error. A page shorter than commentsPerIssuePage is the
// last; a list still going after maxPages, or one that returns a comment
// twice, is refused rather than returned.
func (c *Client) ReadIssueComments(ctx context.Context, number int) ([]IssueComment, error) {
	if number < 1 {
		return nil, fmt.Errorf("%d is not an issue number", number)
	}
	what := fmt.Sprintf("the comments on %s#%d", c.slug, number)
	path := fmt.Sprintf("/repos/%s/issues/%d/comments", c.slug, number)
	query := url.Values{"per_page": {fmt.Sprint(commentsPerIssuePage)}}
	out := []IssueComment{}
	seen := map[int64]bool{}
	for page := 1; ; page++ {
		if page > maxPages {
			return nil, fmt.Errorf("%s did not end after %d pages of %d", what, maxPages, commentsPerIssuePage)
		}
		query.Set("page", fmt.Sprint(page))
		var batch []struct {
			ID   int64 `json:"id"`
			User *struct {
				Login string `json:"login"`
			} `json:"user"`
			Body      string    `json:"body"`
			CreatedAt time.Time `json:"created_at"`
			UpdatedAt time.Time `json:"updated_at"`
		}
		if err := c.call(ctx, http.MethodGet, path+"?"+query.Encode(), nil, &batch); err != nil {
			return nil, classifyRead(err)
		}
		for _, comment := range batch {
			if comment.ID == 0 {
				return nil, fmt.Errorf("GitHub reported a comment in %s with no id", what)
			}
			if seen[comment.ID] {
				return nil, fmt.Errorf("%s returned comment %d twice", what, comment.ID)
			}
			seen[comment.ID] = true
			read := IssueComment{
				ID:        comment.ID,
				Body:      comment.Body,
				CreatedAt: comment.CreatedAt,
				UpdatedAt: comment.UpdatedAt,
			}
			if comment.User != nil {
				read.Author = comment.User.Login
			}
			out = append(out, read)
		}
		if len(batch) < commentsPerIssuePage {
			return out, nil
		}
	}
}

// ReadCheckRuns reads every check run on the commit sha.
func (r *ReadClient) ReadCheckRuns(ctx context.Context, sha string) ([]CheckRun, error) {
	return r.c.ReadCheckRuns(ctx, sha)
}

// ReadCommitStatuses reads the combined status of the commit sha.
func (r *ReadClient) ReadCommitStatuses(ctx context.Context, sha string) (CombinedStatus, error) {
	return r.c.ReadCommitStatuses(ctx, sha)
}

// ReadIssueComments reads every comment on the numbered issue or pull
// request's conversation.
func (r *ReadClient) ReadIssueComments(ctx context.Context, number int) ([]IssueComment, error) {
	return r.c.ReadIssueComments(ctx, number)
}
