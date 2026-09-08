package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/review"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewpost"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun"
)

// mustSlug builds the repository a review would be posted to.
func mustSlug(owner, repo string) review.Slug { return review.Slug{Owner: owner, Repo: repo} }

// stubDoer answers by path, so the order calls are made in is not something
// these tests assert; the client's own suite covers that.
type stubDoer map[string]string

func (d stubDoer) Do(req *http.Request) (*http.Response, error) {
	body, ok := d[req.URL.Path]
	if !ok {
		return &http.Response{
			StatusCode: 404, Header: http.Header{},
			Body: io.NopCloser(strings.NewReader(`{"message": "Not Found"}`)),
		}, nil
	}
	return &http.Response{
		StatusCode: 200, Header: http.Header{},
		Body: io.NopCloser(strings.NewReader(body)),
	}, nil
}

// registration writes a working App registration into a temporary directory.
func registration(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	body := pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	dir := filepath.Join(t.TempDir(), "config")
	if err := githubapp.Initialize(dir, 4242, body); err != nil {
		t.Fatal(err)
	}
	return dir
}

// prRepo builds a repository holding a pull request: a base commit, a head
// commit that changes one file, and a remote carrying refs/pull/7/head.
//
// A real remote and a real pull ref, because resolving a pull request is
// mostly git — fetching the head, anchoring it to the base and reading which
// lines the diff adds — and a fake would be this package's idea of that
// rather than git's.
func prRepo(t *testing.T) (work, baseSHA, headSHA string) {
	t.Helper()
	origin := t.TempDir()
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	setup := func(dir string) {
		run(dir, "init", "-b", "main")
		run(dir, "config", "user.email", "test@example.invalid")
		run(dir, "config", "user.name", "Test")
		// A contributor whose global config signs commits would otherwise
		// need a signing key present for this fixture to commit at all.
		run(dir, "config", "commit.gpgsign", "false")
	}
	write := func(dir, name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	setup(origin)
	write(origin, "a.go", "package a\n\nfunc one() {}\nfunc two() {}\n")
	run(origin, "add", "-A")
	run(origin, "commit", "-m", "base")
	baseSHA = run(origin, "rev-parse", "HEAD")

	write(origin, "a.go", "package a\n\nfunc one() {}\nfunc inserted() {}\nfunc two() {}\n")
	run(origin, "add", "-A")
	run(origin, "commit", "-m", "head")
	headSHA = run(origin, "rev-parse", "HEAD")
	// GitHub publishes every pull request's head at this ref, and it is what
	// a fetch by name asks for.
	run(origin, "update-ref", "refs/pull/7/head", headSHA)
	// The base is what the branch is measured against, so the clone must not
	// already hold the head.
	run(origin, "update-ref", "refs/heads/main", baseSHA)

	work = t.TempDir()
	setup(work)
	run(work, "remote", "add", "origin", origin)
	run(work, "fetch", "--quiet", "origin", "main")
	run(work, "reset", "--hard", "origin/main")
	// A remote that is a local path is not an address a review can be posted
	// to, and the slug is read from it — so it is renamed to one that is,
	// with the fetchable path kept as the URL git actually uses.
	run(work, "config", "remote.origin.url", "git@github.com:acme/widgets.git")
	run(work, "config", "remote.origin.pushurl", origin)
	run(work, "config", "url."+origin+".insteadOf", "git@github.com:acme/widgets.git")
	return work, baseSHA, headSHA
}

// Resolving a pull request has to end with the head in the local repository,
// the change anchored to the base, and the lines a comment may land on read
// from the diff of the two.
func TestResolvingAPullRequestFetchesItsHeadAndReadsItsDiff(t *testing.T) {
	work, baseSHA, headSHA := prRepo(t)
	doer := stubDoer{
		"/repos/acme/widgets/installation":    `{"id": 99}`,
		"/app/installations/99/access_tokens": `{"token": "ghs_x", "expires_at": "2999-01-01T00:00:00Z"}`,
		"/repos/acme/widgets/pulls/7": fmt.Sprintf(
			`{"number": 7, "state": "open", "base": {"sha": %q, "ref": "main"}, "head": {"sha": %q, "ref": "feature/x"}}`,
			baseSHA, headSHA),
	}

	target, err := resolvePullRequest(context.Background(), work, 7,
		clientSeam{dir: registration(t), doer: doer})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if target.slug.String() != "acme/widgets" {
		t.Errorf("the review would be posted to %s", target.slug)
	}
	if target.mergeBase != baseSHA {
		t.Errorf("the change is anchored to %s, want the base %s", target.mergeBase, baseSHA)
	}
	// The head has to be in this repository: the review root is written from
	// its tree, and a review is bound to a commit.
	cmd := exec.Command("git", "cat-file", "-e", headSHA+"^{commit}")
	cmd.Dir = work
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the pull request's head was not fetched: %v\n%s", err, out)
	}
	if !target.added["a.go"][4] {
		t.Errorf("line 4 is inserted by this pull request and is not addable; addable: %v", target.added["a.go"])
	}
	if target.added["a.go"][3] {
		t.Error("an unchanged line is addable, and a comment there is one GitHub would refuse")
	}
}

// A pull request that does not exist is the ordinary typo, and the refusal
// names the repository so somebody can see they are in the wrong checkout.
func TestResolvingAPullRequestThatDoesNotExistNamesTheRepository(t *testing.T) {
	work, _, _ := prRepo(t)
	doer := stubDoer{
		"/repos/acme/widgets/installation":    `{"id": 99}`,
		"/app/installations/99/access_tokens": `{"token": "ghs_x", "expires_at": "2999-01-01T00:00:00Z"}`,
	}
	_, err := resolvePullRequest(context.Background(), work, 7,
		clientSeam{dir: registration(t), doer: doer})
	if err == nil || !strings.Contains(err.Error(), "no pull request 7") {
		t.Fatalf("a missing pull request is reported as %v", err)
	}
}

// A head GitHub reports that the remote does not publish is a pull request
// that moved between the read and the fetch, and reviewing whatever is there
// instead would post a review claiming to describe a commit nobody looked at.
func TestAHeadTheRemoteDoesNotPublishStopsTheReview(t *testing.T) {
	work, baseSHA, _ := prRepo(t)
	absent := strings.Repeat("d", 40)
	doer := stubDoer{
		"/repos/acme/widgets/installation":    `{"id": 99}`,
		"/app/installations/99/access_tokens": `{"token": "ghs_x", "expires_at": "2999-01-01T00:00:00Z"}`,
		"/repos/acme/widgets/pulls/7": fmt.Sprintf(
			`{"number": 7, "base": {"sha": %q, "ref": "main"}, "head": {"sha": %q, "ref": "f"}}`, baseSHA, absent),
	}
	_, err := resolvePullRequest(context.Background(), work, 7,
		clientSeam{dir: registration(t), doer: doer})
	if err == nil {
		t.Fatal("a head the remote does not publish was reviewed anyway")
	}
}

// A preview that printed an empty comment list would read as "this review
// would post nothing", which is the one thing a review must never say by
// accident.
func TestTheEnvelopeNamesItsCommentsAsPendingRatherThanEmpty(t *testing.T) {
	var b bytes.Buffer
	renderEnvelope(&b, &pullRequestTarget{
		slug:  mustSlug("acme", "widgets"),
		pr:    githubapp.PullRequest{Number: 7, HeadSHA: strings.Repeat("2", 40)},
		added: reviewpost.AddedLines{"a.go": {4: true}},
	})
	out := b.String()
	for _, want := range []string{"acme/widgets#7", strings.Repeat("2", 40), "COMMENT", "supplied once", "Nothing was spent"} {
		if !strings.Contains(out, want) {
			t.Errorf("the envelope does not carry %q:\n%s", want, out)
		}
	}
}

// A preview of a post has to be the request itself, comment bodies included:
// one that summarised would not be a preview of what gets sent.
func TestThePayloadPreviewPrintsEveryCommentAndPostsNothing(t *testing.T) {
	target := &pullRequestTarget{
		slug: mustSlug("acme", "widgets"),
		pr:   githubapp.PullRequest{Number: 7, HeadSHA: strings.Repeat("2", 40)},
	}
	payload := githubapp.ReviewPayload{
		CommitID: target.pr.HeadSHA, Event: githubapp.EventComment, Body: "the summary",
		Comments: []githubapp.ReviewComment{
			{Path: "a.go", Line: 4, Side: githubapp.SideRight, Body: "off by one"},
			{Path: "b.go", Line: 9, StartLine: line(7), Side: githubapp.SideRight, Body: "a wide claim"},
		},
	}
	place := reviewpost.Placement{
		Inline:       []reviewrun.Finding{{}, {}},
		Unpositioned: []reviewrun.Finding{{Path: "c.go", StartLine: line(40)}},
	}

	var b bytes.Buffer
	renderPayload(&b, target, payload, place)
	out := b.String()
	for _, want := range []string{
		"POST /repos/acme/widgets/pulls/7/reviews",
		"a.go:4", "b.go:7-9", "off by one", "a wide claim", "the summary",
		"c.go:40 is not a line this pull request adds",
		"Nothing was posted.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the preview does not carry %q:\n%s", want, out)
		}
	}
}

// A finding that was moved to the body is reported as moved. Silently
// relocating it would leave somebody looking for an inline comment that is
// not there.
func TestPlacementIsReportedRatherThanImplied(t *testing.T) {
	var b bytes.Buffer
	renderPlacement(&b, reviewpost.Placement{
		Inline:       []reviewrun.Finding{{}},
		CrossCutting: []reviewrun.Finding{{}, {}},
		Unpositioned: []reviewrun.Finding{{Path: "z.go", StartLine: line(12)}},
	})
	out := b.String()
	if !strings.Contains(out, "4 finding(s): 1 inline, 2 with no line, 1 outside this diff") {
		t.Errorf("the placement is not reported:\n%s", out)
	}
}

// Findings never make the command fail: what a review found is the review's
// content, and a severity floor that blocks is a property of approval rather
// than of running a panel.
func TestOnlyAReviewWithNoVerdictFailsTheCommand(t *testing.T) {
	red := reviewrun.SeverityRed
	found := &reviewrun.Review{Available: true, Findings: []reviewrun.Finding{{Severity: red}}}
	if err := unavailableError(found); err != nil {
		t.Errorf("a review that found something failed the command: %v", err)
	}
	stuck := &reviewrun.Review{Available: false, Reason: "the judge could not be run"}
	if err := unavailableError(stuck); err == nil {
		t.Error("a review that reached no verdict was reported as a success")
	}
}

// A pull request decides the base, the head and the context, so the options
// the pipeline runs under are the ones GitHub reported and not the ones a
// flag might carry.
func TestAPullRequestReviewReadsItsRulesFromTheBaseRef(t *testing.T) {
	target := &pullRequestTarget{
		pr:        githubapp.PullRequest{Number: 7, BaseRef: "main", HeadSHA: strings.Repeat("2", 40)},
		mergeBase: strings.Repeat("1", 40),
	}
	opts := target.options("/repo", reviewTarget{pr: 7, panel: "deep"}, runFlags{})
	if !opts.Context.Posts() {
		t.Error("a pull request review does not run in a context that posts, so its manifest would come from the branch under review")
	}
	if opts.Base != target.mergeBase || opts.Head != target.pr.HeadSHA {
		t.Errorf("the review is measured over %s..%s", opts.Base, opts.Head)
	}
	if opts.BaseLabel != "main" {
		t.Errorf("the review names its base %q, want the ref a reader recognises", opts.BaseLabel)
	}
	if opts.Panel != "deep" {
		t.Error("--panel was dropped")
	}
}

// A preview of a pull request review reads GitHub and writes nothing: it
// starts no run, so it costs nothing, and it makes no request that changes
// anything.
func TestAPullRequestPreviewSpendsNothingAndPostsNothing(t *testing.T) {
	work, baseSHA, headSHA := prRepo(t)
	doer := &countingDoer{inner: stubDoer{
		"/repos/acme/widgets/installation":    `{"id": 99}`,
		"/app/installations/99/access_tokens": `{"token": "ghs_x", "expires_at": "2999-01-01T00:00:00Z"}`,
		"/repos/acme/widgets/pulls/7": fmt.Sprintf(
			`{"number": 7, "state": "open", "base": {"sha": %q, "ref": "main"}, "head": {"sha": %q, "ref": "feature/x"}}`,
			baseSHA, headSHA),
	}}

	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: work}
	cmd := NewRootCmd(env)
	cmd.SetContext(context.Background())
	err := runCodeReviewPR(cmd, env, reviewTarget{pr: 7}, runFlags{dryRun: true},
		clientSeam{dir: registration(t), doer: doer})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	// PATH is not narrowed here: the assertion is that no run was planned into
	// existence, which the plan itself reports.
	body := out.String()
	for _, want := range []string{"panel:", "would be made, and nothing was spent", "acme/widgets#7", "COMMENT", "Nothing was spent and nothing was posted."} {
		if !strings.Contains(body, want) {
			t.Errorf("the preview does not carry %q:\n%s", want, body)
		}
	}
	if doer.writes > 0 {
		t.Errorf("a preview made %d request(s) that change something", doer.writes)
	}
}

// countingDoer counts the requests that write, which is the whole of what a
// preview must not do.
type countingDoer struct {
	inner  stubDoer
	writes int
}

func (d *countingDoer) Do(req *http.Request) (*http.Response, error) {
	// The installation-token mint is a POST that creates nothing on the
	// repository; every other write is one a preview must not make.
	if req.Method != http.MethodGet && !strings.HasSuffix(req.URL.Path, "/access_tokens") {
		d.writes++
	}
	return d.inner.Do(req)
}

// A review of a pull request always runs in the context that posts, which is
// what makes its manifest and its prompts come from the base ref. A flag
// saying otherwise is a contradiction; the flag's own default is not.
func TestAPullRequestRefusesAContextThatContradictsIt(t *testing.T) {
	if err := checkPullRequestFlags(reviewTarget{pr: 7, context: "worktree"}, false); err != nil {
		t.Errorf("the context flag's default was read as a second answer: %v", err)
	}
	if err := checkPullRequestFlags(reviewTarget{pr: 7, context: "worktree"}, true); err == nil {
		t.Error("--context worktree was accepted alongside --pr")
	}
	if err := checkPullRequestFlags(reviewTarget{pr: 7, context: "pr"}, true); err != nil {
		t.Errorf("--context pr contradicts nothing and was refused: %v", err)
	}
}
