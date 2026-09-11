package tests

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/githubapp"
)

// exchange is one scripted request and the answer it gets.
type exchange struct {
	method string
	path   string
	status int
	body   string
	header http.Header
	// err is a transport failure, for the case where the network is what went
	// wrong rather than GitHub.
	err error
}

// scripted is the network, replaced. Each request is matched against the next
// unconsumed exchange, so a client that made a call in the wrong order or made
// one call too many fails rather than passing on a coincidence.
type scripted struct {
	t         *testing.T
	exchanges []exchange
	seen      []*http.Request
	bodies    []string
}

func (s *scripted) Do(req *http.Request) (*http.Response, error) {
	s.t.Helper()
	if len(s.seen) >= len(s.exchanges) {
		s.t.Fatalf("call %d to %s %s was not scripted", len(s.seen)+1, req.Method, req.URL.Path)
	}
	want := s.exchanges[len(s.seen)]
	body := ""
	if req.Body != nil {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			s.t.Fatal(err)
		}
		body = string(raw)
	}
	s.seen = append(s.seen, req)
	s.bodies = append(s.bodies, body)
	if want.method != req.Method || want.path != req.URL.Path {
		s.t.Fatalf("call %d was %s %s, the script expects %s %s",
			len(s.seen), req.Method, req.URL.Path, want.method, want.path)
	}
	if want.err != nil {
		return nil, want.err
	}
	header := want.header
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: want.status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(want.body)),
	}, nil
}

// done reports that every scripted exchange was used. A client that skipped a
// call it was supposed to make is as wrong as one that made an extra.
func (s *scripted) done() {
	s.t.Helper()
	if len(s.seen) != len(s.exchanges) {
		s.t.Errorf("%d of %d scripted calls were made", len(s.seen), len(s.exchanges))
	}
}

const (
	installationPath = "/repos/acme/widgets/installation"
	tokenPath        = "/app/installations/99/access_tokens"
	pullPath         = "/repos/acme/widgets/pulls/7"
	reviewsPath      = "/repos/acme/widgets/pulls/7/reviews"
)

// auth are the two exchanges every authenticated call is preceded by.
func auth(expires time.Time) []exchange {
	return []exchange{
		{method: http.MethodGet, path: installationPath, status: 200, body: `{"id": 99}`},
		{method: http.MethodPost, path: tokenPath, status: 201,
			body: fmt.Sprintf(`{"token": "ghs_scripted", "expires_at": %q}`, expires.Format(time.RFC3339))},
	}
}

func client(t *testing.T, exchanges ...exchange) (*githubapp.Client, *scripted) {
	t.Helper()
	dir := register(t, 4242, pkcs1PEM(t))
	cred, err := githubapp.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	net := &scripted{t: t, exchanges: exchanges}
	return githubapp.NewClient(cred, "acme/widgets",
		githubapp.WithHTTP(net), githubapp.WithBaseURL("https://api.test")), net
}

// The App token proves this process holds the key, and GitHub verifies it with
// the public half. A signature this test cannot verify is one GitHub would
// refuse, so the assertion is the verification rather than the shape.
func TestTheAppTokenIsSignedWithTheRegisteredKey(t *testing.T) {
	c, net := client(t, append(auth(time.Now().Add(time.Hour)),
		exchange{method: http.MethodGet, path: pullPath, status: 200, body: pullBody})...)
	if _, err := c.ReadPullRequest(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	net.done()

	bearer := strings.TrimPrefix(net.seen[0].Header.Get("Authorization"), "Bearer ")
	parts := strings.Split(bearer, ".")
	if len(parts) != 3 {
		t.Fatalf("the App token has %d segments, want 3: %q", len(parts), bearer)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("the signature is not unpadded base64url: %v", err)
	}
	if err := rsa.VerifyPKCS1v15(&testKey.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("GitHub would refuse this App token: %v", err)
	}

	var claims struct {
		Iss string `json:"iss"`
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Iss != "4242" {
		t.Errorf("the App token is issued by %q, want the registered App id", claims.Iss)
	}
	if claims.Iat >= time.Now().Unix() {
		t.Error("the App token's issued-at is not behind the caller's clock, so a machine running fast issues one from the future")
	}
	// GitHub refuses a token whose expiry is more than ten minutes out.
	if life := claims.Exp - claims.Iat; life > 600 {
		t.Errorf("the App token lives %ds counted from its issued-at, and GitHub refuses anything past 600", life)
	}
}

// The installation token reaches every repository the App is installed on, so
// the shortest life it can have is the right one — but re-minting per call
// would spend two requests on every one that matters.
func TestAnInstallationTokenIsMintedOnceAndReused(t *testing.T) {
	c, net := client(t, append(auth(time.Now().Add(time.Hour)),
		exchange{method: http.MethodGet, path: pullPath, status: 200, body: pullBody},
		exchange{method: http.MethodPost, path: reviewsPath, status: 200, body: `{"id": 1, "html_url": "u"}`},
	)...)
	ctx := context.Background()
	if _, err := c.ReadPullRequest(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateReview(ctx, 7, githubapp.ReviewPayload{}); err != nil {
		t.Fatal(err)
	}
	net.done()
	for _, i := range []int{2, 3} {
		if got := net.seen[i].Header.Get("Authorization"); got != "Bearer ghs_scripted" {
			t.Errorf("call %d carries %q, want the minted installation token", i+1, got)
		}
	}
}

// A token close enough to expiry is a hazard: the check and the request that
// uses it are not the same instant, and a review post is the one call that
// must not be retried.
func TestATokenAboutToExpireIsReplacedRatherThanUsed(t *testing.T) {
	now := time.Now()
	script := append(auth(now.Add(30*time.Second)),
		exchange{method: http.MethodGet, path: pullPath, status: 200, body: pullBody})
	// Only the token is re-minted: the installation is a fact about the
	// repository, and re-resolving it would spend a request per call.
	script = append(script,
		exchange{method: http.MethodPost, path: tokenPath, status: 201,
			body: fmt.Sprintf(`{"token": "ghs_second", "expires_at": %q}`, now.Add(time.Hour).Format(time.RFC3339))},
		exchange{method: http.MethodPost, path: reviewsPath, status: 200, body: `{"id": 1}`})

	c, net := client(t, script...)
	ctx := context.Background()
	if _, err := c.ReadPullRequest(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateReview(ctx, 7, githubapp.ReviewPayload{}); err != nil {
		t.Fatal(err)
	}
	net.done()
	if got := net.seen[4].Header.Get("Authorization"); got != "Bearer ghs_second" {
		t.Errorf("the post carries %q, want the re-minted token: a token checked as valid and then used a moment later is the one call that must not be retried", got)
	}
}

// An App that is not installed on the repository is the ordinary first
// failure, and GitHub says it with a 404 that names nothing.
func TestAnUninstalledAppIsToldWhereToInstallIt(t *testing.T) {
	c, net := client(t, exchange{method: http.MethodGet, path: installationPath, status: 404,
		body: `{"message": "Not Found"}`})
	_, err := c.ReadPullRequest(context.Background(), 7)
	if err == nil {
		t.Fatal("a repository the App is not installed on was reviewed anyway")
	}
	if !strings.Contains(err.Error(), "acme/widgets") || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("the refusal does not say the App is not installed on acme/widgets: %v", err)
	}
	net.done()
}

func TestARefusedAppKeyIsReportedAsARefusedKey(t *testing.T) {
	c, _ := client(t, exchange{method: http.MethodGet, path: installationPath, status: 401,
		body: `{"message": "A JSON web token could not be decoded"}`})
	_, err := c.ReadPullRequest(context.Background(), 7)
	if err == nil {
		t.Fatal("a refused App key was not reported")
	}
	if !strings.Contains(err.Error(), "App key") {
		t.Errorf("the refusal does not name the key: %v", err)
	}
}

// GitHub answers 403 to both an exhausted limit and a permission it will not
// grant. Reading the second as the first would tell somebody to wait for a
// reset that is never coming.
func TestARateLimitIsDistinguishedFromARefusal(t *testing.T) {
	reset := time.Now().Add(11 * time.Minute).Unix()
	limited := http.Header{}
	limited.Set("X-RateLimit-Remaining", "0")
	limited.Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))

	c, _ := client(t, append(auth(time.Now().Add(time.Hour)),
		exchange{method: http.MethodGet, path: pullPath, status: 403,
			body: `{"message": "API rate limit exceeded"}`, header: limited})...)
	_, err := c.ReadPullRequest(context.Background(), 7)
	var apiErr *githubapp.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("a rate limit is reported as %T, want a *githubapp.Error", err)
	}
	if !apiErr.RateLimited() {
		t.Error("an exhausted rate limit does not report itself as one")
	}
	if apiErr.RetryAfter.Unix() != reset {
		t.Errorf("the limit resets at %v, want %v", apiErr.RetryAfter.Unix(), reset)
	}

	forbidden := http.Header{}
	forbidden.Set("X-RateLimit-Remaining", "4999")
	c2, _ := client(t, append(auth(time.Now().Add(time.Hour)),
		exchange{method: http.MethodGet, path: pullPath, status: 403,
			body: `{"message": "Resource not accessible by integration"}`, header: forbidden})...)
	_, err = c2.ReadPullRequest(context.Background(), 7)
	if !errors.As(err, &apiErr) {
		t.Fatalf("a permission refusal is reported as %T", err)
	}
	if apiErr.RateLimited() {
		t.Error("a permission refusal reports itself as a rate limit, so a person is told to wait for a reset that never comes")
	}
}

// A secondary rate limit reports itself with Retry-After and no remaining
// count, which is the shape a burst of comments actually hits.
func TestASecondaryRateLimitIsRecognisedFromRetryAfter(t *testing.T) {
	header := http.Header{}
	header.Set("Retry-After", "60")
	c, _ := client(t, append(auth(time.Now().Add(time.Hour)),
		exchange{method: http.MethodPost, path: reviewsPath, status: 403,
			body: `{"message": "You have exceeded a secondary rate limit"}`, header: header})...)
	_, err := c.CreateReview(context.Background(), 7, githubapp.ReviewPayload{})
	var apiErr *githubapp.Error
	if !errors.As(err, &apiErr) || !apiErr.RateLimited() {
		t.Fatalf("a secondary rate limit is not recognised: %v", err)
	}
}

// One comment on a line outside the diff rejects the whole review, and the
// detail naming which one is in errors[] rather than in message.
func TestA422RepeatsTheDetailThatNamesTheRefusedComment(t *testing.T) {
	c, _ := client(t, append(auth(time.Now().Add(time.Hour)),
		exchange{method: http.MethodPost, path: reviewsPath, status: 422, body: `{
			"message": "Validation Failed",
			"errors": [{"resource": "PullRequestReviewComment", "field": "line", "code": "invalid",
			            "message": "line must be part of the diff"}]
		}`})...)
	_, err := c.CreateReview(context.Background(), 7, githubapp.ReviewPayload{})
	if err == nil {
		t.Fatal("a rejected review was reported as posted")
	}
	if !strings.Contains(err.Error(), "line must be part of the diff") {
		t.Errorf("the refusal drops the detail that names what GitHub refused: %v", err)
	}
}

func TestAnErrorBodyThatIsNotJSONIsStillReported(t *testing.T) {
	c, _ := client(t, exchange{method: http.MethodGet, path: installationPath, status: 502,
		body: "<html>bad gateway</html>"})
	_, err := c.ReadPullRequest(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "bad gateway") {
		t.Fatalf("a non-JSON refusal is not reported: %v", err)
	}
}

// A network that fails mid-post is not a refusal GitHub made, and reporting it
// as one would send somebody looking at their App's permissions.
func TestATransportFailureIsReportedAsOne(t *testing.T) {
	c, _ := client(t, append(auth(time.Now().Add(time.Hour)),
		exchange{method: http.MethodPost, path: reviewsPath, err: errors.New("connection reset by peer")})...)
	_, err := c.CreateReview(context.Background(), 7, githubapp.ReviewPayload{})
	if err == nil {
		t.Fatal("a review that never left the machine was reported as posted")
	}
	var apiErr *githubapp.Error
	if errors.As(err, &apiErr) {
		t.Errorf("a connection failure is reported as a refusal GitHub made: %v", err)
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("the failure does not name what went wrong: %v", err)
	}
}

const pullBody = `{
	"number": 7, "state": "open", "title": "A change", "draft": false,
	"base": {"sha": "1111111111111111111111111111111111111111", "ref": "main"},
	"head": {"sha": "2222222222222222222222222222222222222222", "ref": "feature/x"}
}`

func TestAPullRequestIsReadWithItsCommits(t *testing.T) {
	c, net := client(t, append(auth(time.Now().Add(time.Hour)),
		exchange{method: http.MethodGet, path: pullPath, status: 200, body: pullBody})...)
	pr, err := c.ReadPullRequest(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if pr.HeadSHA != "2222222222222222222222222222222222222222" || pr.BaseRef != "main" {
		t.Errorf("read %+v", pr)
	}
}

func TestAPullRequestThatDoesNotExistIsNamed(t *testing.T) {
	c, _ := client(t, append(auth(time.Now().Add(time.Hour)),
		exchange{method: http.MethodGet, path: pullPath, status: 404, body: `{"message": "Not Found"}`})...)
	_, err := c.ReadPullRequest(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "no pull request 7") {
		t.Fatalf("a missing pull request is reported as %v", err)
	}
}

// A pull request with no head commit is one no review could be bound to, and
// a review that is not bound to a commit cannot be evidence that a commit was
// reviewed.
func TestAPullRequestWithNoHeadCommitIsRefused(t *testing.T) {
	c, _ := client(t, append(auth(time.Now().Add(time.Hour)),
		exchange{method: http.MethodGet, path: pullPath, status: 200, body: `{"number": 7}`})...)
	if _, err := c.ReadPullRequest(context.Background(), 7); err == nil {
		t.Fatal("a pull request with no head commit was accepted")
	}
}

func TestAPullRequestNumberBelowOneNeverReachesTheNetwork(t *testing.T) {
	c, net := client(t)
	if _, err := c.ReadPullRequest(context.Background(), 0); err == nil {
		t.Fatal("0 was accepted as a pull request number")
	}
	net.done()
}

// A JSON consumer branching on the comment list must not have to tell null
// from empty, and GitHub reads a null there as a malformed field.
func TestAReviewWithNoInlineCommentsSendsAnEmptyList(t *testing.T) {
	c, net := client(t, append(auth(time.Now().Add(time.Hour)),
		exchange{method: http.MethodPost, path: reviewsPath, status: 200, body: `{"id": 5, "html_url": "u"}`})...)
	posted, err := c.CreateReview(context.Background(), 7, githubapp.ReviewPayload{
		CommitID: "abc", Event: githubapp.EventComment, Body: "nothing found",
	})
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if posted.HTMLURL != "u" {
		t.Errorf("the posted review is %+v", posted)
	}

	var sent map[string]json.RawMessage
	if err := json.Unmarshal([]byte(net.bodies[2]), &sent); err != nil {
		t.Fatalf("the request body is not JSON: %v\n%s", err, net.bodies[2])
	}
	if !bytes.Equal(sent["comments"], []byte("[]")) {
		t.Errorf("comments was sent as %s, want []", sent["comments"])
	}
	if string(sent["event"]) != `"COMMENT"` {
		t.Errorf("event was sent as %s; a review run posts COMMENT and nothing else", sent["event"])
	}
}

// Approval is a separate act with its own subcommand, and no code path from a
// review run reaches it. The event a post carries is where that would first
// stop being true.
func TestTheOnlyEventAReviewRunPostsIsComment(t *testing.T) {
	if githubapp.EventComment != "COMMENT" {
		t.Fatalf("a review run posts %q", githubapp.EventComment)
	}
}
