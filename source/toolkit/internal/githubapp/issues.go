package githubapp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ErrNotFound is what a 404 on an issue read is reported as.
//
// GitHub answers 404 both to a resource that does not exist and to one the
// token cannot see, so this means "not visible to this token" as much as "does
// not exist". Matched with errors.Is; errors.As still reaches the *Error under
// it.
var ErrNotFound = errors.New("GitHub reports no such resource, or none this token can see")

// ErrForbidden is what a 403 on an issue read is reported as, when the 403 is
// a refusal rather than a rate limit.
//
// A rate-limited 403 is not this: it carries no judgement about the token, and
// a caller that read it as one would report an outage as a denial. That one
// stays a plain *Error whose RateLimited reports true.
var ErrForbidden = errors.New("GitHub refused this token access")

// classifyRead marks a refusal GitHub made to a read with the sentinel a
// caller tells it apart by, and leaves every other failure as it is.
func classifyRead(err error) error {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case apiErr.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	case apiErr.StatusCode == http.StatusForbidden && !apiErr.RateLimited():
		return fmt.Errorf("%w: %w", ErrForbidden, err)
	}
	return err
}

// Issue is one issue as GitHub's REST API reports it.
type Issue struct {
	Number int
	Title  string
	// State is "open" or "closed".
	State string
	// StateReason is why the issue is in its state — "completed",
	// "not_planned", "reopened" — and empty when GitHub records none.
	StateReason string
	// Author is the login of the account that opened the issue, and empty
	// when that account has been deleted.
	Author string
	// Labels are the names of the labels the issue carries.
	Labels []string
	// PullRequest reports that the number is a pull request. REST serves pull
	// requests through the issue endpoints, so every issue read can return
	// one.
	PullRequest bool
}

// issuePayload is the part of a REST issue that Issue carries.
type issuePayload struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	State       string `json:"state"`
	StateReason string `json:"state_reason"`
	User        *struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	PullRequest *struct{} `json:"pull_request"`
}

func (p issuePayload) issue() Issue {
	out := Issue{
		Number:      p.Number,
		Title:       p.Title,
		State:       p.State,
		StateReason: p.StateReason,
		PullRequest: p.PullRequest != nil,
	}
	if p.User != nil {
		out.Author = p.User.Login
	}
	for _, label := range p.Labels {
		out.Labels = append(out.Labels, label.Name)
	}
	return out
}

// issuesPerPage bounds one page of a paged issue read.
//
// REST returns every issue whole, body included, and an issue body may be
// 65536 characters. A page that overruns maxBody arrives as a JSON parse
// failure, which refuses the read rather than returning a short list, so the
// page is kept small enough that the ordinary case never meets the cap.
const issuesPerPage = 25

// ReadIssue fetches one issue.
func (c *Client) ReadIssue(ctx context.Context, number int) (Issue, error) {
	if number < 1 {
		return Issue{}, fmt.Errorf("%d is not an issue number", number)
	}
	var payload issuePayload
	path := fmt.Sprintf("/repos/%s/issues/%d", c.slug, number)
	if err := c.call(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return Issue{}, classifyRead(err)
	}
	if payload.Number == 0 {
		return Issue{}, fmt.Errorf("GitHub reported issue %d on %s with no number", number, c.slug)
	}
	return payload.issue(), nil
}

// Permission is one account's access to a repository.
type Permission struct {
	// Permission is GitHub's coarse level: "admin", "write", "read" or
	// "none". It folds maintain into write and triage into read.
	Permission string
	// RoleName is the role as granted — "admin", "maintain", "write",
	// "triage", "read", or a custom role's name — and is what tells maintain
	// and triage apart from the levels they fold into.
	RoleName string
}

// ReadPermission fetches login's collaborator permission on the repository.
func (c *Client) ReadPermission(ctx context.Context, login string) (Permission, error) {
	if strings.TrimSpace(login) == "" {
		return Permission{}, errors.New("no login to read a permission for")
	}
	var payload struct {
		Permission string `json:"permission"`
		RoleName   string `json:"role_name"`
	}
	path := fmt.Sprintf("/repos/%s/collaborators/%s/permission", c.slug, url.PathEscape(login))
	if err := c.call(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return Permission{}, classifyRead(err)
	}
	if payload.Permission == "" {
		return Permission{}, fmt.Errorf("GitHub reported no permission for %s on %s", login, c.slug)
	}
	return Permission{Permission: payload.Permission, RoleName: payload.RoleName}, nil
}

// ReadLabelledIssues reads every open issue carrying label.
//
// One label per call: REST filters by exact label names and reads a comma as
// a list of labels that must all be present, so a prefix cannot be asked for
// and a label containing a comma cannot be asked for at all. Pull requests
// carrying the label are included, with PullRequest set.
func (c *Client) ReadLabelledIssues(ctx context.Context, label string) ([]Issue, error) {
	if strings.TrimSpace(label) == "" {
		return nil, errors.New("no label to read issues by")
	}
	if strings.Contains(label, ",") {
		return nil, fmt.Errorf("label %q contains a comma, which GitHub reads as two labels", label)
	}
	query := url.Values{"state": {"open"}, "labels": {label}}
	return c.readIssuePages(ctx, fmt.Sprintf("/repos/%s/issues", c.slug), query,
		fmt.Sprintf("the open issues labelled %q on %s", label, c.slug))
}

// ReadPredecessors reads every issue the numbered one is blocked by, each with
// its state and state reason.
func (c *Client) ReadPredecessors(ctx context.Context, number int) ([]Issue, error) {
	if number < 1 {
		return nil, fmt.Errorf("%d is not an issue number", number)
	}
	return c.readIssuePages(ctx, fmt.Sprintf("/repos/%s/issues/%d/dependencies/blocked_by", c.slug, number), url.Values{},
		fmt.Sprintf("the issues blocking %s#%d", c.slug, number))
}

// readIssuePages reads a paged list of issues to its end.
//
// The whole list or an error. A page shorter than issuesPerPage is the last,
// and a list still going after maxPages is refused rather than returned short:
// a list read as complete when it is not is a blocker nobody sees.
func (c *Client) readIssuePages(ctx context.Context, path string, query url.Values, what string) ([]Issue, error) {
	var out []Issue
	for page := 1; ; page++ {
		if page > maxPages {
			return nil, fmt.Errorf("%s did not end after %d pages of %d", what, maxPages, issuesPerPage)
		}
		query.Set("per_page", fmt.Sprint(issuesPerPage))
		query.Set("page", fmt.Sprint(page))
		var batch []issuePayload
		if err := c.call(ctx, http.MethodGet, path+"?"+query.Encode(), nil, &batch); err != nil {
			return nil, classifyRead(err)
		}
		for _, payload := range batch {
			out = append(out, payload.issue())
		}
		if len(batch) < issuesPerPage {
			return out, nil
		}
	}
}

// ReadParent reads the issue the numbered one is a sub-issue of. found is
// false, with no error, when it has no parent.
//
// GitHub answers 404 both to an issue with no parent and to an issue that
// does not exist or that the token cannot see. A 404 is therefore followed by
// a read of the issue itself, and is "no parent" only when that read succeeds;
// otherwise the issue read's own failure is returned.
func (c *Client) ReadParent(ctx context.Context, number int) (parent Issue, found bool, err error) {
	return c.readParent(ctx, number, false)
}

// ReadParentOfVisibleIssue reads the parent of an issue the caller has
// already read with this client, such as one returned by ReadLabelledIssues.
// That read settles that the issue exists and is visible, so a 404 is "no
// parent" and no further request is made.
//
// A caller holding only a number uses ReadParent: given an issue that is not
// there, this reports it as having no parent.
func (c *Client) ReadParentOfVisibleIssue(ctx context.Context, number int) (parent Issue, found bool, err error) {
	return c.readParent(ctx, number, true)
}

// readParent reads the parent of the numbered issue. visible says the caller
// has already read the issue, so a 404 needs no read of the issue to tell
// "no parent" apart from "no issue".
func (c *Client) readParent(ctx context.Context, number int, visible bool) (Issue, bool, error) {
	if number < 1 {
		return Issue{}, false, fmt.Errorf("%d is not an issue number", number)
	}
	var payload issuePayload
	path := fmt.Sprintf("/repos/%s/issues/%d/parent", c.slug, number)
	if err := c.call(ctx, http.MethodGet, path, nil, &payload); err != nil {
		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
			return Issue{}, false, classifyRead(err)
		}
		if visible {
			return Issue{}, false, nil
		}
		if _, err := c.ReadIssue(ctx, number); err != nil {
			return Issue{}, false, err
		}
		return Issue{}, false, nil
	}
	if payload.Number == 0 {
		return Issue{}, false, fmt.Errorf("GitHub reported a parent of %s#%d with no number", c.slug, number)
	}
	return payload.issue(), true, nil
}

// ReadIssue fetches one issue.
func (r *ReadClient) ReadIssue(ctx context.Context, number int) (Issue, error) {
	return r.c.ReadIssue(ctx, number)
}

// ReadPermission fetches login's collaborator permission on the repository.
func (r *ReadClient) ReadPermission(ctx context.Context, login string) (Permission, error) {
	return r.c.ReadPermission(ctx, login)
}

// ReadLabelledIssues reads every open issue carrying label.
func (r *ReadClient) ReadLabelledIssues(ctx context.Context, label string) ([]Issue, error) {
	return r.c.ReadLabelledIssues(ctx, label)
}

// ReadPredecessors reads every issue the numbered one is blocked by.
func (r *ReadClient) ReadPredecessors(ctx context.Context, number int) ([]Issue, error) {
	return r.c.ReadPredecessors(ctx, number)
}

// ReadParent reads the issue the numbered one is a sub-issue of, and reports
// found false, with no error, when it has none.
func (r *ReadClient) ReadParent(ctx context.Context, number int) (Issue, bool, error) {
	return r.c.ReadParent(ctx, number)
}

// ReadParentOfVisibleIssue reads the parent of an issue already read with
// this client, and reports found false, with no error, on a 404.
func (r *ReadClient) ReadParentOfVisibleIssue(ctx context.Context, number int) (Issue, bool, error) {
	return r.c.ReadParentOfVisibleIssue(ctx, number)
}
