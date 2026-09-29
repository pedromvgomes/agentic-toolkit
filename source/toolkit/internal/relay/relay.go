// Package relay has a GitHub Actions workflow in another repository post or
// approve a review, for a machine that holds no App registration.
//
// The workflow's runner holds the App's credential and makes every call that
// writes to the reviewed repository, as a registered machine would: it posts a
// review the caller already computed, or decides and grants an approval
// itself. The caller's token only starts that run and reads how it ended, so
// nothing reaches the reviewed repository under the caller's identity.
//
// A workflow_dispatch is accepted before the run it starts can be read, so
// starting a run and learning its outcome are two steps: Dispatch, then Await.
package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// baseURL is github.com's API.
	baseURL = "https://api.github.com"

	// apiVersion is the REST API version every call pins. It matches
	// githubapp.APIVersion, so the relay is read against the same schema the
	// rest of the binary is.
	apiVersion = "2022-11-28"

	// userAgent identifies the caller. GitHub refuses a request without one.
	userAgent = "agtk-code-review"

	// maxBody bounds how much of a response is read.
	maxBody = 1 << 20

	// workflowFile is the relay repository's workflow, addressed by file name.
	workflowFile = "relay.yml"

	// ref is the branch the relay's workflow runs from. A relay repository's
	// default branch must be named main: dispatching by name rather than by
	// asking GitHub which branch is the default costs one request fewer on
	// every dispatch, at the cost of this one required name.
	ref = "main"
)

// The actions the relay's workflow accepts.
const (
	ActionRun     = "run"
	ActionApprove = "approve"
)

// DefaultTimeout bounds one Await. A relay run is a job on a runner GitHub has
// to schedule and set up first, so the bound is mostly the time to reach it; a
// wait cut short reports that it stopped, never how the run ended.
const DefaultTimeout = 20 * time.Minute

// Doer is the seam the network is reached through, satisfied by *http.Client.
//
// The same shape as githubapp.Doer, declared here so a package that carries a
// caller's token does not import the package that holds the App's key.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Target names the relay repository and the token that reaches it.
//
// Token is the caller's own, needing only to dispatch and read the relay's
// workflow runs. It is never the App's credential, and it never touches the
// repository under review.
type Target struct {
	Slug  string
	Token string
}

// Request is what one dispatch asks the relay to do.
type Request struct {
	// Repo is the owner/name of the repository under review.
	Repo   string
	PR     int
	Action string
	// Panel overrides the review's panel, and applies only to ActionRun.
	Panel string
	// Payload is the review the relay posts, already encoded as JSON by the
	// caller. It is required for ActionRun and refused for ActionApprove,
	// which the relay's runner decides from the pull request itself.
	//
	// Opaque here: this package carries a caller's token and never learns the
	// shape of what the App posts, so decoding it is left to the command the
	// relay's runner feeds it to.
	Payload string
}

// Dispatched is what a dispatch leaves to find its run by.
type Dispatched struct {
	Request Request
	// Since is when the dispatch was sent. A run created before it is not
	// this dispatch's, however its title reads.
	Since time.Time
	// RunID and URL name the run GitHub started, and are empty when GitHub
	// accepted the dispatch without naming it.
	RunID int64
	URL   string
}

// Result is how a completed relay run ended.
type Result struct {
	// Conclusion is GitHub's word for it: "success", "failure", "cancelled",
	// "timed_out" and so on. A run that completed and failed is still a
	// Result; an error from Await means the outcome was never learned.
	Conclusion string
	// URL is the run's page, for a person to open.
	URL string
}

// Error is a refusal GitHub made, carrying enough to say which one.
//
// Separate from githubapp.Error because the two answer different questions: a
// refusal here is about the caller's token on the relay repository, never
// about the App.
type Error struct {
	StatusCode int
	Method     string
	Path       string
	Message    string
	// RetryAfter is when a rate limit this request hit resets, and is zero
	// when the refusal was not one.
	RetryAfter time.Time
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("GitHub answered %d to %s %s", e.StatusCode, e.Method, e.Path)
	}
	return fmt.Sprintf("GitHub answered %d to %s %s: %s", e.StatusCode, e.Method, e.Path, e.Message)
}

// Dispatch starts the relay's workflow for req, and returns once GitHub has
// accepted it, not once the run finishes.
//
// It asks GitHub to name the run it started. When GitHub accepts without
// naming one, Await finds the run by its title and Since instead.
func Dispatch(ctx context.Context, doer Doer, target Target, req Request) (Dispatched, error) {
	if err := target.check(); err != nil {
		return Dispatched{}, err
	}
	if err := req.check(); err != nil {
		return Dispatched{}, err
	}
	inputs := map[string]string{
		"repo":   req.Repo,
		"pr":     strconv.Itoa(req.PR),
		"action": req.Action,
	}
	if req.Panel != "" {
		inputs["panel"] = req.Panel
	}
	if req.Payload != "" {
		inputs["payload"] = req.Payload
	}
	body := struct {
		Ref              string            `json:"ref"`
		Inputs           map[string]string `json:"inputs"`
		ReturnRunDetails bool              `json:"return_run_details"`
	}{Ref: ref, Inputs: inputs, ReturnRunDetails: true}

	since := time.Now()
	var started struct {
		ID      int64  `json:"workflow_run_id"`
		HTMLURL string `json:"html_url"`
	}
	path := "/repos/" + target.Slug + "/actions/workflows/" + workflowFile + "/dispatches"
	if err := call(ctx, doer, target.Token, http.MethodPost, path, body, &started); err != nil {
		return Dispatched{}, fmt.Errorf("dispatch the relay on %s: %w", target.Slug, err)
	}
	return Dispatched{Request: req, Since: since, RunID: started.ID, URL: started.HTMLURL}, nil
}

// slugPattern is an owner/name pair. Both halves are spliced into a request
// path, so anything that would change the path's shape is refused here.
var slugPattern = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)

func (t Target) check() error {
	if !slugPattern.MatchString(t.Slug) {
		return fmt.Errorf("the relay repository %q is not owner/name", t.Slug)
	}
	if strings.TrimSpace(t.Token) == "" {
		return errors.New("no GitHub token to reach the relay with")
	}
	return nil
}

func (r Request) check() error {
	if !slugPattern.MatchString(r.Repo) {
		return fmt.Errorf("the repository to review, %q, is not owner/name", r.Repo)
	}
	if r.PR <= 0 {
		return fmt.Errorf("%d is not a pull request number", r.PR)
	}
	switch r.Action {
	case ActionRun, ActionApprove:
	default:
		return fmt.Errorf("the relay has no action %q: it takes %q or %q", r.Action, ActionRun, ActionApprove)
	}
	if r.Panel != "" && r.Action != ActionRun {
		return fmt.Errorf("a panel applies only to %q, not %q", ActionRun, r.Action)
	}
	// A run with nothing to post would have the relay post nothing and report
	// success, and an approval carrying a payload would have one the relay
	// never reads.
	if r.Action == ActionRun && strings.TrimSpace(r.Payload) == "" {
		return fmt.Errorf("%q hands the relay a review to post, and this request carries none", ActionRun)
	}
	if r.Action == ActionApprove && r.Payload != "" {
		return fmt.Errorf("%q is decided by the relay from the pull request, and takes no payload", ActionApprove)
	}
	return nil
}

// call makes one request with the caller's token. An empty answer leaves out
// untouched, which is how a 204 reads.
func call(ctx context.Context, doer Doer, token, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("build the %s %s request: %w", method, path, err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build the %s %s request: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := doer.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return fmt.Errorf("read GitHub's answer to %s %s: %w", method, path, err)
	}
	if res.StatusCode >= 300 {
		return &Error{
			StatusCode: res.StatusCode, Method: method, Path: path, Message: apiMessage(raw),
			RetryAfter: rateLimitReset(res),
		}
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("read GitHub's answer to %s %s: %w", method, path, err)
	}
	return nil
}

// apiMessage pulls the human-readable half out of an error body, falling back
// to the body itself when it is not the shape GitHub documents.
func apiMessage(raw []byte) string {
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Message == "" {
		return strings.TrimSpace(string(raw))
	}
	return payload.Message
}

// rateLimitReset reads when a rate limit res answered with resets, or the
// zero time when it did not answer one.
//
// A primary limit reports its remaining count at zero and the reset instant
// in X-RateLimit-Reset; a secondary limit reports neither and answers
// Retry-After instead, which is read first because a response carrying it is
// a secondary limit whatever X-RateLimit-Remaining says.
func rateLimitReset(res *http.Response) time.Time {
	if after := res.Header.Get("Retry-After"); after != "" {
		if secs, err := strconv.Atoi(after); err == nil {
			return time.Now().Add(time.Duration(secs) * time.Second)
		}
	}
	if res.Header.Get("X-RateLimit-Remaining") != "0" {
		return time.Time{}
	}
	reset, err := strconv.ParseInt(res.Header.Get("X-RateLimit-Reset"), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(reset, 0)
}
