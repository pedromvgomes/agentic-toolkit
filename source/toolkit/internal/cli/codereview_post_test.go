package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

// postedReviewURL is the page of the review postNet accepts.
const postedReviewURL = "https://github.test/r/1"

// postNet answers everything `code-review post` asks GitHub as the App,
// refusing the file-level comments on the paths named, and records every
// request with its body.
type postNet struct {
	comments *fileCommentDoer
	calls    []string
	// review is the body the review was posted with, and is empty when none
	// was.
	review string
}

func newPostNet(head string, refuse map[string]bool, reviewAccepted bool) *postNet {
	rest := stubDoer{
		"/repos/acme/widgets/installation":    `{"id": 99}`,
		"/app/installations/99/access_tokens": `{"token": "ghs_x", "expires_at": "2999-01-01T00:00:00Z"}`,
		"/repos/acme/widgets/pulls/7": fmt.Sprintf(
			`{"number": 7, "state": "open", "base": {"sha": %q, "ref": "main"}, "head": {"sha": %q, "ref": "feature/x"}}`,
			strings.Repeat("1", 40), head),
	}
	if reviewAccepted {
		rest["/repos/acme/widgets/pulls/7/reviews"] = `{"id": 1, "html_url": "` + postedReviewURL + `", "state": "COMMENTED"}`
	}
	return &postNet{comments: &fileCommentDoer{rest: rest, refuse: refuse}}
}

func (n *postNet) Do(req *http.Request) (*http.Response, error) {
	n.calls = append(n.calls, req.Method+" "+req.URL.Path)
	if req.Method == http.MethodPost && req.URL.Path == "/repos/acme/widgets/pulls/7/reviews" {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		n.review = string(raw)
		req.Body = io.NopCloser(bytes.NewReader(raw))
	}
	return n.comments.Do(req)
}

// writes are the requests that changed something on the pull request.
func (n *postNet) writes() []string {
	var writes []string
	for _, call := range n.calls {
		if strings.HasPrefix(call, http.MethodPost+" /repos/") {
			writes = append(writes, call)
		}
	}
	return writes
}

// reviewToPost is a review of head carrying one inline comment and a
// file-level comment on each of paths.
func reviewToPost(head string, paths ...string) relayedReview {
	in := relayedReview{
		Review: githubapp.ReviewPayload{
			CommitID: head, Body: "## Review by `agtk`\n\nOne finding.", Event: githubapp.EventComment,
			Comments: []githubapp.ReviewComment{{Path: "a.go", Line: 4, Side: "RIGHT", Body: "off by one"}},
		},
		FileComments: []githubapp.FileComment{},
	}
	for _, path := range paths {
		in.FileComments = append(in.FileComments, githubapp.FileComment{
			CommitID: head, Path: path, SubjectType: githubapp.SubjectFile, Body: "two reasons to change",
		})
	}
	return in
}

// encoded is in as `code-review post` reads it.
func encoded(t *testing.T, in relayedReview) string {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// postReview runs `code-review post --pr 7` with stdin through a seam and
// returns what it wrote.
func postReview(t *testing.T, work string, stdin io.Reader, seam clientSeam) (string, error) {
	t.Helper()
	var out bytes.Buffer
	env := &Env{Stdin: stdin, Stdout: &out, Stderr: io.Discard, WorkDir: work}
	cmd := NewRootCmd(env)
	cmd.SetContext(context.Background())
	err := runCodeReviewPost(cmd, env, 7, seam)
	return out.String(), err
}

// watchedReader records whether anything was read from it.
type watchedReader struct {
	r    io.Reader
	read bool
}

func (w *watchedReader) Read(p []byte) (int, error) {
	w.read = true
	return w.r.Read(p)
}

func TestPostIsASubcommandOfCodeReview(t *testing.T) {
	env := &Env{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard, WorkDir: t.TempDir()}
	cmd, _, err := NewRootCmd(env).Find([]string{"code-review", "post"})
	if err != nil || cmd.Name() != "post" {
		t.Fatalf("code-review post resolves to %v: %v", cmd, err)
	}
}

// A review read from standard input is posted as the App exactly as it was
// computed: the review with its inline comments, then one request per
// file-level comment, and the review's address is what is reported.
func TestPostPostsTheReviewAndEachFileCommentAsTheApp(t *testing.T) {
	work, _, headSHA := prRepo(t)
	net := newPostNet(headSHA, nil, true)
	in := reviewToPost(headSHA, "a.go", "b.go")

	out, err := postReview(t, work, strings.NewReader(encoded(t, in)), clientSeam{dir: registration(t), doer: net})
	if err != nil {
		t.Fatalf("post: %v\n%s", err, out)
	}
	if !strings.Contains(out, postedReviewURL) || !strings.Contains(out, "acme/widgets#7") {
		t.Errorf("the post does not name the review it made:\n%s", out)
	}
	want := []string{
		"POST /repos/acme/widgets/pulls/7/reviews",
		"POST /repos/acme/widgets/pulls/7/comments",
		"POST /repos/acme/widgets/pulls/7/comments",
	}
	if got := net.writes(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("post made %v, want %v", got, want)
	}
	var sent githubapp.ReviewPayload
	if err := json.Unmarshal([]byte(net.review), &sent); err != nil {
		t.Fatalf("the posted review is not JSON: %v\n%s", err, net.review)
	}
	if sent.CommitID != headSHA || sent.Event != githubapp.EventComment || sent.Body != in.Review.Body ||
		len(sent.Comments) != 1 || sent.Comments[0] != in.Review.Comments[0] {
		t.Errorf("the review posted was %+v, want %+v", sent, in.Review)
	}
	if got := strings.Join(net.comments.paths, ","); got != "a.go file,b.go file" {
		t.Errorf("the file-level comments posted were %s, want a.go then b.go", got)
	}
}

// Posting takes this machine's own registration and nothing else: a machine
// holding none is refused as `run --pr` and `approve` are, before standard
// input is read or GitHub asked anything, and never offered a relay: it is
// what a relay's runner runs.
func TestPostOnAMachineHoldingNoRegistrationRefusesBeforeReadingAnything(t *testing.T) {
	work, _, headSHA := prRepo(t)
	net := newPostNet(headSHA, nil, true)
	stdin := &watchedReader{r: strings.NewReader(encoded(t, reviewToPost(headSHA)))}

	out, err := postReview(t, work, stdin, clientSeam{dir: unregistered(t), doer: net, getenv: environment(relayed())})
	if err == nil {
		t.Fatalf("an unregistered machine posted:\n%s", out)
	}
	if !strings.Contains(err.Error(), "this machine holds no GitHub App registration: run `agtk code-review register` to register one") {
		t.Errorf("the refusal is not the registration's own: %v", err)
	}
	if strings.Contains(err.Error(), "AGTK_CODE_REVIEW_RELAY") {
		t.Errorf("post offered the relay: %v", err)
	}
	if stdin.read {
		t.Error("an unregistered machine read the review before refusing")
	}
	if len(net.calls) > 0 || out != "" {
		t.Errorf("an unregistered machine still sent %v and wrote %q", net.calls, out)
	}
}

// Whatever is wrong with standard input is reported as standard input, before
// GitHub is asked anything, so it never reads as a registration or a network
// failure. An event other than COMMENT is among them: a review posted from
// here must never be how the App approves.
func TestPostRefusesAReviewItCannotPostAsComputed(t *testing.T) {
	work, _, headSHA := prRepo(t)
	dir := registration(t)

	unknownField := strings.Replace(encoded(t, reviewToPost(headSHA)), `"review":`, `"panel":"deep","review":`, 1)
	changes := reviewToPost(headSHA)
	changes.Review.Event = "REQUEST_CHANGES"
	pending := reviewToPost(headSHA)
	pending.Review.Event = ""
	noCommit := reviewToPost(headSHA)
	noCommit.Review.CommitID = ""
	strayComment := reviewToPost(headSHA, "a.go")
	strayComment.FileComments[0].CommitID = strings.Repeat("9", 40)

	for name, tc := range map[string]struct {
		stdin, want string
	}{
		"empty":                       {"", "carries no review"},
		"not JSON":                    {"review: yes", "not the JSON"},
		"an unknown field":            {unknownField, "unknown field"},
		"two values":                  {encoded(t, reviewToPost(headSHA)) + "{}", "more than one JSON value"},
		"no commit":                   {encoded(t, noCommit), "names no commit_id"},
		"requesting changes":          {encoded(t, changes), `"REQUEST_CHANGES"`},
		"no event":                    {encoded(t, pending), `event ""`},
		"a comment on another commit": {encoded(t, strayComment), strings.Repeat("9", 40)},
	} {
		net := newPostNet(headSHA, nil, true)
		out, err := postReview(t, work, strings.NewReader(tc.stdin), clientSeam{dir: dir, doer: net})
		if err == nil {
			t.Errorf("%s: posted:\n%s", name, out)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "standard input") {
			t.Errorf("%s: refused as %v, want it to name standard input and say %q", name, err, tc.want)
		}
		if strings.Contains(err.Error(), "regist") {
			t.Errorf("%s: a malformed review reads as a registration problem: %v", name, err)
		}
		if len(net.calls) > 0 {
			t.Errorf("%s: a malformed review still sent %v", name, net.calls)
		}
	}
}

// A review made against a head the pull request has since moved past describes
// code it no longer holds. It is refused, naming both commits and what fixes
// it, and nothing is posted.
func TestPostRefusesAReviewOfAHeadThePullRequestHasMovedPast(t *testing.T) {
	work, _, headSHA := prRepo(t)
	stale := strings.Repeat("3", 40)
	net := newPostNet(headSHA, nil, true)

	out, err := postReview(t, work, strings.NewReader(encoded(t, reviewToPost(stale, "a.go"))),
		clientSeam{dir: registration(t), doer: net})
	if err == nil {
		t.Fatalf("a review of a stale head was posted:\n%s", out)
	}
	for _, want := range []string{stale, headSHA, "acme/widgets#7", "nothing was posted", "run --pr 7"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
	if writes := net.writes(); len(writes) > 0 {
		t.Errorf("a review of a stale head still made %v", writes)
	}
}

// Once the review has landed, a file-level comment GitHub refuses costs its own
// thread and nothing else, as it does on a registered run: the rest are still
// posted, the lost one is named, and the command succeeds.
func TestPostReportsARefusedFileCommentWithoutFailing(t *testing.T) {
	work, _, headSHA := prRepo(t)
	net := newPostNet(headSHA, map[string]bool{"a.go": true}, true)

	out, err := postReview(t, work, strings.NewReader(encoded(t, reviewToPost(headSHA, "a.go", "b.go"))),
		clientSeam{dir: registration(t), doer: net})
	if err != nil {
		t.Fatalf("a refused file-level comment failed the post: %v\n%s", err, out)
	}
	if len(net.comments.paths) != 2 {
		t.Errorf("a refused comment stopped the ones after it: %v", net.comments.paths)
	}
	for _, want := range []string{postedReviewURL, "a.go", "carry no thread"} {
		if !strings.Contains(out, want) {
			t.Errorf("the post does not report %q:\n%s", want, out)
		}
	}
}

// A review GitHub refuses fails the command, and no file-level comment follows
// it: those hang off a review that never landed.
func TestPostFailsWhenTheReviewItselfIsRefused(t *testing.T) {
	work, _, headSHA := prRepo(t)
	net := newPostNet(headSHA, nil, false)

	out, err := postReview(t, work, strings.NewReader(encoded(t, reviewToPost(headSHA, "a.go"))),
		clientSeam{dir: registration(t), doer: net})
	if err == nil || !strings.Contains(err.Error(), "post the review to acme/widgets#7") {
		t.Fatalf("a refused review was reported as %v:\n%s", err, out)
	}
	if len(net.comments.paths) > 0 {
		t.Errorf("file-level comments followed a review that never landed: %v", net.comments.paths)
	}
}
