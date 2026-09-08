package githubapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is github.com's API. A GitHub Enterprise host is a different
// origin and is not addressed here.
const DefaultBaseURL = "https://api.github.com"

// APIVersion is the REST API GitHub serves when asked. Pinned, because the
// unpinned default is whatever is current, and a review's inline comments are
// the part of this that a schema change would break silently.
const APIVersion = "2022-11-28"

// UserAgent identifies the poster. GitHub refuses a request without one.
const UserAgent = "agtk-code-review"

// maxBody bounds how much of a response is read. An error body is an error
// message; anything past this is not information a person is going to act on,
// and reading an unbounded stream from the network into memory is not a thing
// a CLI should do because the server is usually well behaved.
const maxBody = 1 << 20

// Doer is the seam the network is reached through, satisfied by
// *http.Client. Tests script responses rather than serve them.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client makes authenticated calls as the App's installation on one
// repository.
//
// The installation token it mints is held in memory for the life of the
// process and never written anywhere. It reaches every repository the App is
// installed on, so the shortest life it can have is the right one.
type Client struct {
	cred    *Credential
	http    Doer
	baseURL string
	now     func() time.Time

	// installation is the App's installation on the repository this client
	// was built for, resolved once.
	slug         string
	installation int64
	token        string
	tokenExpiry  time.Time
}

// Option configures a Client.
type Option func(*Client)

// WithHTTP replaces the transport.
func WithHTTP(d Doer) Option { return func(c *Client) { c.http = d } }

// WithBaseURL points the client at another API origin.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimSuffix(u, "/") }
}

// NewClient builds a client that acts as the App's installation on slug.
func NewClient(cred *Credential, slug string, opts ...Option) *Client {
	c := &Client{
		cred:    cred,
		http:    &http.Client{Timeout: 30 * time.Second},
		baseURL: DefaultBaseURL,
		now:     time.Now,
		slug:    slug,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Error is a refusal GitHub made, carrying enough to say which one.
type Error struct {
	StatusCode int
	Method     string
	Path       string
	Message    string
	// RetryAfter is when the rate limit this request hit resets, and is zero
	// when the refusal was not a rate limit.
	RetryAfter time.Time
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("GitHub answered %d to %s %s", e.StatusCode, e.Method, e.Path)
	}
	return fmt.Sprintf("GitHub answered %d to %s %s: %s", e.StatusCode, e.Method, e.Path, e.Message)
}

// RateLimited reports whether this refusal was a rate limit rather than a
// judgement about the request.
//
// GitHub answers 403 to both an exhausted limit and a permission it will not
// grant, so the status alone does not separate them; the reset header does.
func (e *Error) RateLimited() bool { return !e.RetryAfter.IsZero() }

// do makes one request, with a bearer token the caller chose.
func (c *Client) do(ctx context.Context, method, path, bearer string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("build the %s %s request: %w", method, path, err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build the %s %s request: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", APIVersion)
	req.Header.Set("User-Agent", UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.Do(req)
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
			StatusCode: res.StatusCode,
			Method:     method,
			Path:       path,
			Message:    apiMessage(raw),
			RetryAfter: rateLimitReset(res),
		}
	}
	if out == nil {
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
		Errors  []struct {
			Resource string `json:"resource"`
			Field    string `json:"field"`
			Code     string `json:"code"`
			Message  string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Message == "" {
		return strings.TrimSpace(string(raw))
	}
	msg := payload.Message
	// The detail is where a 422 says which comment it refused, and it is the
	// only part of the answer that names a line.
	for _, e := range payload.Errors {
		detail := e.Message
		if detail == "" {
			detail = strings.TrimSpace(e.Resource + " " + e.Field + " " + e.Code)
		}
		if detail != "" {
			msg += "; " + detail
		}
	}
	return msg
}

// rateLimitReset reads when an exhausted limit recovers, and returns the zero
// time for a refusal that was not one.
func rateLimitReset(res *http.Response) time.Time {
	if res.Header.Get("X-RateLimit-Remaining") != "0" {
		// A secondary rate limit reports itself with Retry-After and no
		// remaining count, so it is read second rather than not at all.
		if after := res.Header.Get("Retry-After"); after != "" {
			if secs, err := parseInt(after); err == nil {
				return time.Now().Add(time.Duration(secs) * time.Second)
			}
		}
		return time.Time{}
	}
	reset, err := parseInt(res.Header.Get("X-RateLimit-Reset"))
	if err != nil {
		return time.Time{}
	}
	return time.Unix(reset, 0)
}

// bearer returns an installation token, minting one when none is held or the
// one held is close enough to expiry to be a hazard.
func (c *Client) bearer(ctx context.Context) (string, error) {
	// Refreshed before the token actually expires, because the check and the
	// request that uses it are not the same instant and a review post is the
	// one call that must not be retried.
	if c.token != "" && c.now().Add(tokenMargin).Before(c.tokenExpiry) {
		return c.token, nil
	}
	jwt, err := c.cred.appJWT(c.now())
	if err != nil {
		return "", err
	}
	if c.installation == 0 {
		var found struct {
			ID int64 `json:"id"`
		}
		path := "/repos/" + c.slug + "/installation"
		if err := c.do(ctx, http.MethodGet, path, jwt, nil, &found); err != nil {
			return "", installationError(c.slug, err)
		}
		c.installation = found.ID
	}
	var minted struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	path := fmt.Sprintf("/app/installations/%d/access_tokens", c.installation)
	if err := c.do(ctx, http.MethodPost, path, jwt, nil, &minted); err != nil {
		return "", err
	}
	if minted.Token == "" {
		return "", fmt.Errorf("GitHub issued an empty installation token for %s", c.slug)
	}
	c.token, c.tokenExpiry = minted.Token, minted.ExpiresAt
	return c.token, nil
}

// tokenMargin is how much life a held token must have left to be reused.
const tokenMargin = time.Minute

// installationError turns "the App is not installed here" into the sentence
// that fixes it, and leaves every other refusal alone.
func installationError(slug string, err error) error {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusNotFound:
			return fmt.Errorf("the GitHub App is not installed on %s: install it on that repository, then run this again", slug)
		case http.StatusUnauthorized:
			return fmt.Errorf("GitHub refused this machine's App key: %w", err)
		}
	}
	return err
}

// call makes one authenticated request as the installation.
func (c *Client) call(ctx context.Context, method, path string, body, out any) error {
	token, err := c.bearer(ctx)
	if err != nil {
		return err
	}
	return c.do(ctx, method, path, token, body, out)
}
