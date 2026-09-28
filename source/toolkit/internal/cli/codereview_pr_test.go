package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
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
	}, reviewrun.ThreadsRead(nil))
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
		Unattachable: []reviewrun.Finding{{Path: "c.go", StartLine: line(40)}},
	}

	var b bytes.Buffer
	renderPayload(&b, target, payload, place)
	out := b.String()
	for _, want := range []string{
		"POST /repos/acme/widgets/pulls/7/reviews",
		"a.go:4", "b.go:7-9", "off by one", "a wide claim", "the summary",
		"c.go:40 carries no thread to answer on",
		"Nothing was posted.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the preview does not carry %q:\n%s", want, out)
		}
	}
}

// Where each finding ended up is reported rather than implied. A finding
// stated in the body with no thread beside it looks exactly like one that got
// a comment, and the two oblige opposite things before this head is approved.
func TestPlacementIsReportedRatherThanImplied(t *testing.T) {
	var b bytes.Buffer
	renderPlacement(&b, reviewpost.Placement{
		Inline:       []reviewrun.Finding{{}},
		FileLevel:    []reviewrun.Finding{{}, {}},
		Unattachable: []reviewrun.Finding{{Path: "z.go", StartLine: line(12)}},
	})
	out := b.String()
	if !strings.Contains(out, "4 finding(s): 1 inline, 2 against a whole file, 1 with nowhere to answer") {
		t.Errorf("the placement is not reported:\n%s", out)
	}
}

// A prompt-injection finding agtk could not attach closes approval outright,
// with no reply that opens it. Somebody reading the run has to be told that
// now rather than discovering it when approval refuses.
func TestADeadlockedInjectionFindingIsReportedByTheRun(t *testing.T) {
	var b bytes.Buffer
	renderPlacement(&b, reviewpost.Placement{
		Unattachable: []reviewrun.Finding{{Category: reviewrun.CategoryPromptInjection}},
	})
	if out := b.String(); !strings.Contains(out, "Approval is closed until the code changes") {
		t.Errorf("an unattachable injection finding is not reported as closing approval:\n%s", out)
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
	opts := target.options("/repo", reviewTarget{pr: 7, panel: "deep"}, runFlags{}, reviewrun.Threads{})
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
	// A read is not always a GET. The installation-token mint creates nothing
	// on the repository, and every GraphQL query is a POST — so the method
	// alone would count reading the existing threads as changing something.
	// A GraphQL mutation is still a write, and is recognised as one rather
	// than exempted along with the queries.
	switch {
	case req.Method == http.MethodGet:
	case strings.HasSuffix(req.URL.Path, "/access_tokens"):
	case req.URL.Path == "/graphql" && !graphQLMutation(req):
	default:
		d.writes++
	}
	return d.inner.Do(req)
}

// graphQLMutation reports whether a GraphQL request asks to change something.
//
// The body is put back, because reading it here is an inspection and the
// request still has to be sent.
func graphQLMutation(req *http.Request) bool {
	if req.Body == nil {
		return false
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return true
	}
	req.Body = io.NopCloser(bytes.NewReader(raw))
	var payload struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(payload.Query), "mutation")
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

// review builds a finished review for the reporting tests.
func reviewFor(available bool, findings ...reviewrun.Finding) *reviewrun.Review {
	return &reviewrun.Review{
		Panel: "standard", Manifest: "builtin", Range: "main..HEAD",
		Available: available, Reason: map[bool]string{true: "", false: "the judge could not be run"}[available],
		Findings: findings,
		Reports: []reviewrun.RunReport{{
			Label: "correctness", Role: reviewrun.RoleReviewer, Provider: "claudecode",
			Report: reviewrun.Answered(findings),
		}},
	}
}

// A review that reached no verdict has to fail the command whether or not the
// caller asked for JSON. A --json branch that returned early is how the exit
// status and the output format came apart.
func TestAReviewWithNoVerdictFailsTheCommandUnderJSONToo(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		var out bytes.Buffer
		env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: t.TempDir()}
		t2 := &pullRequestTarget{
			slug: mustSlug("acme", "widgets"),
			pr:   githubapp.PullRequest{Number: 7, HeadSHA: strings.Repeat("2", 40)},
		}
		result := reviewFor(false)
		payload, place := reviewpost.Build(result, t2.pr, nil)

		if err := reportReview(env, t2, result, payload, place, nil, nil, asJSON); err != nil {
			t.Fatalf("json=%v: report: %v", asJSON, err)
		}
		if err := unavailableError(result); err == nil {
			t.Errorf("json=%v: a review that reached no verdict was reported as a success", asJSON)
		}
	}
}

// A --json consumer must be able to tell a review that reached no verdict from
// one that found nothing: both produce an empty comment list.
func TestThePostedJSONCarriesWhetherTheReviewReachedAVerdict(t *testing.T) {
	target := &pullRequestTarget{
		slug: mustSlug("acme", "widgets"),
		pr:   githubapp.PullRequest{Number: 7, HeadSHA: strings.Repeat("2", 40)},
	}
	result := reviewFor(false)
	payload, place := reviewpost.Build(result, target.pr, nil)

	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: t.TempDir()}
	if err := reportReview(env, target, result, payload, place, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Available bool   `json:"available"`
		Reason    string `json:"reason"`
		Posted    bool   `json:"posted"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("the output is not JSON: %v\n%s", err, out.String())
	}
	if got.Available {
		t.Error("a review that reached no verdict reports itself as available")
	}
	if got.Reason == "" {
		t.Error("the JSON does not say why the review reached no verdict")
	}
	if got.Posted {
		t.Error("an unposted review reports itself as posted")
	}
}

// A caller reading --json has to be able to tell a review that stayed
// unavailable because it was blocked from one that failed ordinarily, and
// runCodeReviewPR routes on exactly this field to withhold the post.
func TestAPostedJSONCarriesWhetherTheReviewWasBlocked(t *testing.T) {
	target := &pullRequestTarget{
		slug: mustSlug("acme", "widgets"),
		pr:   githubapp.PullRequest{Number: 7, HeadSHA: strings.Repeat("2", 40)},
	}
	result := reviewFor(false)
	result.Blocked = true
	result.FallbackFrom = "standard"
	payload, place := reviewpost.Build(result, target.pr, nil)

	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: t.TempDir()}
	if err := reportReview(env, target, result, payload, place, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Blocked      bool   `json:"blocked"`
		FallbackFrom string `json:"fallback_from"`
		Posted       bool   `json:"posted"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("the output is not JSON: %v\n%s", err, out.String())
	}
	if !got.Blocked {
		t.Error("a blocked review does not report itself as blocked")
	}
	if got.FallbackFrom != "standard" {
		t.Errorf("the fallback panel is not reported: got %q", got.FallbackFrom)
	}
	if got.Posted {
		t.Error("a blocked review reports itself as posted")
	}
}

// A --json consumer parsing this stream must not be handed prose because the
// post is what failed.
func TestAFailedPostStillReportsAsJSONUnderJSON(t *testing.T) {
	target := &pullRequestTarget{
		slug: mustSlug("acme", "widgets"),
		pr:   githubapp.PullRequest{Number: 7, HeadSHA: strings.Repeat("2", 40)},
	}
	result := reviewFor(true, reviewrun.Finding{
		Path: "a.go", StartLine: line(4), EndLine: line(4),
		Category: "correctness", Severity: reviewrun.SeverityRed,
		Issue: "off by one", Evidence: "i <= len(x)", Reviewer: "correctness",
	})
	payload, place := reviewpost.Build(result, target.pr, reviewpost.AddedLines{"a.go": {4: true}})

	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: t.TempDir()}
	if err := reportReview(env, target, result, payload, place, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out.Bytes()) {
		t.Fatalf("a failed post under --json wrote something that is not JSON:\n%s", out.String())
	}
}

// A comment is attached at the end of its region, so that is the line GitHub
// validates and the line a finding is refused for. Naming the start would
// report a line that is on the diff as the reason the finding is not.
func TestPlacementNamesTheLineThatWasActuallyRefused(t *testing.T) {
	f := reviewrun.Finding{Path: "z.go", StartLine: line(10), EndLine: line(20), Category: "correctness"}
	added := reviewpost.AddedLines{"a.go": {10: true}}
	_, place := reviewpost.Build(reviewFor(true, f), githubapp.PullRequest{HeadSHA: "x"}, added)
	if len(place.Unattachable) != 1 {
		t.Fatalf("placed as %+v", place)
	}

	var b bytes.Buffer
	renderPlacement(&b, place)
	out := b.String()
	if !strings.Contains(out, "z.go:20") {
		t.Errorf("the refused line is not named:\n%s", out)
	}
	if strings.Contains(out, "z.go:10") {
		t.Errorf("the region's start is named as the line the comment was refused for:\n%s", out)
	}
}

// fileCommentDoer answers the file-comment endpoint, refusing the paths named.
type fileCommentDoer struct {
	rest   stubDoer
	refuse map[string]bool
	// paths is every path a comment was attempted for, in order.
	paths []string
}

func (d *fileCommentDoer) Do(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost || !strings.HasSuffix(req.URL.Path, "/pulls/7/comments") {
		return d.rest.Do(req)
	}
	var sent struct {
		Path        string `json:"path"`
		SubjectType string `json:"subject_type"`
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &sent); err != nil {
		return nil, err
	}
	d.paths = append(d.paths, sent.Path+" "+sent.SubjectType)
	if d.refuse[sent.Path] {
		return &http.Response{StatusCode: 422, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(
			`{"message": "Validation Failed", "errors": [{"message": "pull_request_review_thread.path could not be resolved"}]}`))}, nil
	}
	return &http.Response{StatusCode: 201, Header: http.Header{},
		Body: io.NopCloser(strings.NewReader(`{"id": 5, "html_url": "https://github.test/c/5"}`))}, nil
}

// A finding that hangs off a whole file gets one request of its own, because
// subject_type is not a field a review's draft comments carry.
func TestEachFileLevelFindingGetsOneRequestOfItsOwn(t *testing.T) {
	work, baseSHA, headSHA := prRepo(t)
	doer := &fileCommentDoer{rest: stubDoer{
		"/repos/acme/widgets/installation":    `{"id": 99}`,
		"/app/installations/99/access_tokens": `{"token": "ghs_x", "expires_at": "2999-01-01T00:00:00Z"}`,
		"/repos/acme/widgets/pulls/7": fmt.Sprintf(
			`{"number": 7, "state": "open", "base": {"sha": %q, "ref": "main"}, "head": {"sha": %q, "ref": "feature/x"}}`,
			baseSHA, headSHA),
	}}
	target, err := resolvePullRequest(context.Background(), work, 7,
		clientSeam{dir: registration(t), doer: doer})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	place := reviewpost.Placement{FileLevel: []reviewrun.Finding{
		{Path: "a.go", Category: "architecture", Severity: reviewrun.SeverityAmber, Issue: "two reasons to change"},
		{Path: "b.go", Category: "correctness", Severity: reviewrun.SeverityRed, Issue: "off by one"},
	}}
	failures := postFileComments(context.Background(), target, place)

	if len(failures) != 0 {
		t.Errorf("comments GitHub accepted are reported as refused: %+v", failures)
	}
	want := []string{"a.go file", "b.go file"}
	if len(doer.paths) != len(want) {
		t.Fatalf("%d requests were made, want one per finding: %v", len(doer.paths), doer.paths)
	}
	for i, path := range want {
		if doer.paths[i] != path {
			t.Errorf("request %d was %q, want %q", i, doer.paths[i], path)
		}
	}
}

// These fail one at a time and cost nothing but themselves, so one refusal
// never stops the rest — and the finding it lost has to be named, because a
// finding stated in the body with no thread beside it looks exactly like one
// agtk chose not to attach.
func TestOneRefusedFileCommentNeitherStopsTheRestNorGoesUnreported(t *testing.T) {
	work, baseSHA, headSHA := prRepo(t)
	doer := &fileCommentDoer{
		rest: stubDoer{
			"/repos/acme/widgets/installation":    `{"id": 99}`,
			"/app/installations/99/access_tokens": `{"token": "ghs_x", "expires_at": "2999-01-01T00:00:00Z"}`,
			"/repos/acme/widgets/pulls/7": fmt.Sprintf(
				`{"number": 7, "state": "open", "base": {"sha": %q, "ref": "main"}, "head": {"sha": %q, "ref": "feature/x"}}`,
				baseSHA, headSHA),
		},
		refuse: map[string]bool{"a.go": true},
	}
	target, err := resolvePullRequest(context.Background(), work, 7,
		clientSeam{dir: registration(t), doer: doer})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	place := reviewpost.Placement{FileLevel: []reviewrun.Finding{
		{Path: "a.go", Category: "architecture", Severity: reviewrun.SeverityAmber},
		{Path: "b.go", Category: "correctness", Severity: reviewrun.SeverityRed},
	}}
	failures := postFileComments(context.Background(), target, place)

	if len(doer.paths) != 2 {
		t.Errorf("a refused comment stopped the ones after it: %v", doer.paths)
	}
	if len(failures) != 1 || failures[0].Path != "a.go" {
		t.Fatalf("the refusal was reported as %+v", failures)
	}

	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: work}
	reportFileComments(env, failures, false)
	for _, want := range []string{"a.go", "carry no thread", "--force"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the report does not carry %q:\n%s", want, out.String())
		}
	}
}

// bearerDoer answers as the doer it wraps and records which path each request
// asked for and which token it carried.
type bearerDoer struct {
	next  githubapp.Doer
	calls []string
}

func (d *bearerDoer) Do(req *http.Request) (*http.Response, error) {
	d.calls = append(d.calls, req.URL.Path+" "+req.Header.Get("Authorization"))
	return d.next.Do(req)
}

// environment answers a token lookup from vars rather than from the process,
// so a token the test runner happens to hold never decides an outcome.
func environment(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

// unregistered is a registration directory holding nothing.
func unregistered(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "config")
}

// explainPR runs `explain --pr 7` through a seam and returns what it wrote.
func explainPR(t *testing.T, work string, seam clientSeam) (string, error) {
	t.Helper()
	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: work}
	cmd := NewRootCmd(env)
	cmd.SetContext(context.Background())
	err := runCodeReviewExplain(cmd, env, reviewTarget{pr: 7, context: "worktree"}, false, seam)
	return out.String(), err
}

// headFetched reports whether the pull request's head reached the local
// repository, which it does only once the pull request has been read.
func headFetched(t *testing.T, work, head string) bool {
	t.Helper()
	cmd := exec.Command("git", "cat-file", "-e", head+"^{commit}")
	cmd.Dir = work
	return cmd.Run() == nil
}

// GH_TOKEN is looked at first and GITHUB_TOKEN second, and a variable holding
// only whitespace is one that is not set.
func TestTheReadTokenComesFromGHTokenThenGitHubToken(t *testing.T) {
	for _, tc := range []struct {
		vars            map[string]string
		token, variable string
	}{
		{map[string]string{"GH_TOKEN": "gh", "GITHUB_TOKEN": "github"}, "gh", "GH_TOKEN"},
		{map[string]string{"GH_TOKEN": " \n", "GITHUB_TOKEN": " github\n"}, "github", "GITHUB_TOKEN"},
		{map[string]string{"GITHUB_TOKEN": "github"}, "github", "GITHUB_TOKEN"},
		{map[string]string{}, "", ""},
	} {
		token, variable := clientSeam{getenv: environment(tc.vars)}.token()
		if token != tc.token || variable != tc.variable {
			t.Errorf("%v gave %q from %q, want %q from %q", tc.vars, token, variable, tc.token, tc.variable)
		}
	}
}

// A container holding no App registration can still say which panel a pull
// request would get, reading it with the token its environment carries — and
// only with that token: nothing asks GitHub for an installation token.
func TestExplainPRReadsWithAnEnvironmentTokenOnAMachineHoldingNoRegistration(t *testing.T) {
	work, baseSHA, headSHA := prRepo(t)
	doer := &bearerDoer{next: prDoer(baseSHA, headSHA, reviewsOf(), noThreads)}

	out, err := explainPR(t, work, clientSeam{
		dir: unregistered(t), doer: doer,
		getenv: environment(map[string]string{"GH_TOKEN": "  ghp_env\n"}),
	})
	if err != nil {
		t.Fatalf("explain --pr with only GH_TOKEN: %v", err)
	}
	for _, want := range []string{"context: pr", "panel:   quick-codex", headSHA} {
		if !strings.Contains(out, want) {
			t.Errorf("explain --pr did not report %q:\n%s", want, out)
		}
	}
	if len(doer.calls) == 0 {
		t.Fatal("explain --pr read nothing from GitHub")
	}
	for _, call := range doer.calls {
		if !strings.HasSuffix(call, " Bearer ghp_env") {
			t.Errorf("a request was not made with the environment's token: %s", call)
		}
	}
}

// A registered machine reads as the App whether or not the environment holds a
// token, and says exactly the same thing either way.
func TestARegisteredMachineReadsAsTheAppWhateverTheEnvironmentHolds(t *testing.T) {
	work, baseSHA, headSHA := prRepo(t)
	dir := registration(t)

	var outputs []string
	for _, vars := range []map[string]string{
		{},
		{"GH_TOKEN": "ghp_env", "GITHUB_TOKEN": "ghs_actions"},
	} {
		doer := &bearerDoer{next: prDoer(baseSHA, headSHA, reviewsOf(), noThreads)}
		out, err := explainPR(t, work, clientSeam{dir: dir, doer: doer, getenv: environment(vars)})
		if err != nil {
			t.Fatalf("explain --pr with %v: %v", vars, err)
		}
		outputs = append(outputs, out)
		for _, call := range doer.calls {
			if strings.Contains(call, "ghp_env") || strings.Contains(call, "ghs_actions") {
				t.Errorf("a registered machine read with the environment's token: %s", call)
			}
			if strings.HasPrefix(call, "/repos/acme/widgets/pulls/7 ") && !strings.HasSuffix(call, " Bearer ghs_x") {
				t.Errorf("the pull request was not read with the installation token: %s", call)
			}
		}
	}
	if outputs[0] != outputs[1] {
		t.Errorf("a token in the environment changed what a registered machine reports:\n--- without ---\n%s\n--- with ---\n%s",
			outputs[0], outputs[1])
	}
}

// A review is posted and a head approved only as the App, since a review under
// any other identity is invisible to the reads that decide what was reviewed.
// Both refuse on a machine holding no registration whatever the environment
// carries, and before anything is read: no request reaches GitHub and the head
// is never fetched, so no panel can have been started.
func TestPostingAndApprovingRefuseWithoutARegistrationBeforeAnythingIsRead(t *testing.T) {
	for _, vars := range []map[string]string{
		{},
		{"GH_TOKEN": "ghp_env"},
	} {
		work, baseSHA, headSHA := prRepo(t)
		doer := &bearerDoer{next: prDoer(baseSHA, headSHA, reviewsOf(), noThreads)}
		seam := clientSeam{dir: unregistered(t), doer: doer, getenv: environment(vars)}

		var out bytes.Buffer
		env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: work}
		cmd := NewRootCmd(env)
		cmd.SetContext(context.Background())

		runErr := runCodeReviewPR(cmd, env, reviewTarget{pr: 7}, runFlags{}, seam)
		approveErr := runCodeReviewApprove(cmd, env, 7, seam)
		for command, err := range map[string]error{"run --pr": runErr, "approve": approveErr} {
			if err == nil {
				t.Errorf("%s with %v was not refused for want of a registration", command, vars)
				continue
			}
			// Both ways out are named: registering this machine, or naming a
			// relay that posts as the App from somewhere else.
			for _, want := range []string{"code-review register", "AGTK_CODE_REVIEW_RELAY", "acme/widgets"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%s with %v is refused without naming %q: %v", command, vars, want, err)
				}
			}
		}

		if len(doer.calls) > 0 {
			t.Errorf("with %v, a refused post still asked GitHub for %v", vars, doer.calls)
		}
		if headFetched(t, work, headSHA) {
			t.Errorf("with %v, a refused post still fetched the head a panel would review", vars)
		}
		if out.Len() > 0 {
			t.Errorf("with %v, a refused post still reported something:\n%s", vars, out.String())
		}
	}
}

// A read with neither a registration nor a token names both ways to fix it.
func TestExplainPRWithNeitherARegistrationNorATokenNamesBoth(t *testing.T) {
	work, _, _ := prRepo(t)
	_, err := explainPR(t, work, clientSeam{dir: unregistered(t), getenv: environment(nil)})
	if err == nil {
		t.Fatal("explain --pr read a pull request with nothing to read it with")
	}
	for _, want := range []string{"code-review register", "GH_TOKEN", "GITHUB_TOKEN"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// A registration somebody started and never finished is answered by finishing
// it. Reading around it with a token would hide the breakage until the first
// command that posts, so the token is not used and nothing is read.
func TestAHalfWrittenRegistrationIsNotReadAroundWithAToken(t *testing.T) {
	work, baseSHA, headSHA := prRepo(t)
	dir := registration(t)
	if err := os.Remove(filepath.Join(dir, githubapp.KeyFile)); err != nil {
		t.Fatal(err)
	}
	doer := &bearerDoer{next: prDoer(baseSHA, headSHA, reviewsOf(), noThreads)}
	seam := clientSeam{dir: dir, doer: doer, getenv: environment(map[string]string{"GH_TOKEN": "ghp_env"})}

	_, err := explainPR(t, work, seam)
	if err == nil {
		t.Fatal("explain --pr read around a half-written registration")
	}
	for _, want := range []string{"no private key", "register` again"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}

	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: work}
	cmd := NewRootCmd(env)
	cmd.SetContext(context.Background())
	err = runCodeReviewPR(cmd, env, reviewTarget{pr: 7}, runFlags{dryRun: true}, seam)
	if err == nil || !strings.Contains(err.Error(), "register` again") {
		t.Errorf("run --pr --dry-run read around a half-written registration: %v", err)
	}

	if len(doer.calls) > 0 {
		t.Errorf("a half-written registration still sent %v", doer.calls)
	}
}

// GitHub answers a token that cannot see a repository as though the pull
// request were not there. The refusal names the token it read with, and says
// nothing about a registration, which is not what is missing.
func TestATokenThatCannotSeeTheRepositoryIsNotReportedAsAMissingRegistration(t *testing.T) {
	work, _, _ := prRepo(t)
	doer := &bearerDoer{next: stubDoer{}}
	_, err := explainPR(t, work, clientSeam{
		dir: unregistered(t), doer: doer,
		getenv: environment(map[string]string{"GITHUB_TOKEN": "ghs_other_repo"}),
	})
	if err == nil {
		t.Fatal("a pull request the token cannot see was explained")
	}
	for _, want := range []string{"acme/widgets#7", "GITHUB_TOKEN", "cannot see the repository"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "regist") {
		t.Errorf("a token without access is reported as a registration problem: %v", err)
	}
}

// A zero-value clientSeam is what production code builds: getenv reads the
// real environment, and options carries no transport override, so both
// fall through to what a live process actually has.
func TestAZeroValueClientSeamReadsTheRealEnvironmentAndTransport(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "ghp_from_the_real_environment")
	token, variable := (clientSeam{}).token()
	if token != "ghp_from_the_real_environment" || variable != "GITHUB_TOKEN" {
		t.Errorf("a zero-value seam read %q from %q, want ghp_from_the_real_environment from GITHUB_TOKEN", token, variable)
	}

	if opts := (clientSeam{}).options(); opts != nil {
		t.Errorf("a zero-value seam's options is %v, want nil", opts)
	}

	t.Setenv("AGTK_CODE_REVIEW_RELAY", "acme/relay")
	if repo, _ := (clientSeam{}).relay(); repo != "acme/relay" {
		t.Errorf("a zero-value seam read the relay %q, want acme/relay", repo)
	}
	if _, ok := (clientSeam{}).relayDoer().(*http.Client); !ok {
		t.Errorf("a zero-value seam reaches the relay through %T, want a real HTTP client", (clientSeam{}).relayDoer())
	}
}

// A repository readPullRequest cannot even name is refused before any network
// call, the same way resolvePullRequest is.
func TestReadPullRequestFailsBeforeAnythingWithNoRemote(t *testing.T) {
	work := t.TempDir()
	doer := &bearerDoer{next: stubDoer{}}
	_, err := readPullRequest(context.Background(), work, 7,
		clientSeam{dir: unregistered(t), doer: doer, getenv: environment(nil)})
	if err == nil {
		t.Fatal("readPullRequest succeeded with no git repository to name a remote from")
	}
	if len(doer.calls) > 0 {
		t.Errorf("a repository that cannot be identified still reached GitHub: %v", doer.calls)
	}
}

// relayRunURL is the page of the run relayNet reports starting.
const relayRunURL = "https://github.com/acme/relay/actions/runs/555"

// relayNet answers the relay repository's dispatch and the read of the run it
// names, and hands every other request to next.
//
// Every request is recorded, so a test can say which repository was asked for
// what, and under which token.
type relayNet struct {
	// next answers what is not the relay's, and is nil when nothing else
	// should be asked for; such a request is then answered 404.
	next githubapp.Doer
	// refuse is the status the dispatch is answered with, and zero accepts it.
	refuse int
	// conclusion is how the run ends.
	conclusion string

	calls []string
	// dispatched is the body the dispatch was sent with.
	dispatched string
}

func (n *relayNet) Do(req *http.Request) (*http.Response, error) {
	n.calls = append(n.calls, req.Method+" "+req.URL.Path+" "+req.Header.Get("Authorization"))
	answer := func(status int, body string) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	switch {
	case req.Method == http.MethodPost && req.URL.Path == "/repos/acme/relay/actions/workflows/relay.yml/dispatches":
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		n.dispatched = string(raw)
		if n.refuse != 0 {
			return answer(n.refuse, `{"message": "Not Found"}`)
		}
		return answer(http.StatusOK, `{"workflow_run_id": 555, "html_url": "`+relayRunURL+`"}`)
	case req.Method == http.MethodGet && req.URL.Path == "/repos/acme/relay/actions/runs/555":
		return answer(http.StatusOK, fmt.Sprintf(
			`{"id": 555, "status": "completed", "conclusion": %q, "html_url": %q, "display_title": "relayed", "created_at": "2026-09-28T12:00:00Z"}`,
			n.conclusion, relayRunURL))
	case n.next != nil:
		return n.next.Do(req)
	}
	return answer(http.StatusNotFound, `{"message": "Not Found"}`)
}

// relayCalls are the requests that reached the relay repository.
func (n *relayNet) relayCalls() []string {
	var calls []string
	for _, call := range n.calls {
		if strings.Contains(call, " /repos/acme/relay/") {
			calls = append(calls, call)
		}
	}
	return calls
}

// relayInputs decodes the inputs the relay was dispatched with.
func relayInputs(t *testing.T, n *relayNet) map[string]string {
	t.Helper()
	var body struct {
		Inputs map[string]string `json:"inputs"`
	}
	if err := json.Unmarshal([]byte(n.dispatched), &body); err != nil {
		t.Fatalf("the dispatch body is not JSON: %v\n%s", err, n.dispatched)
	}
	return body.Inputs
}

// relayed is an environment naming the relay and a token to reach it with.
func relayed() map[string]string {
	return map[string]string{"GH_TOKEN": "ghp_env", "AGTK_CODE_REVIEW_RELAY": " acme/relay\n"}
}

// postPR runs `run --pr 7` through a seam and returns what it wrote.
func postPR(t *testing.T, work string, target reviewTarget, flags runFlags, seam clientSeam) (string, error) {
	t.Helper()
	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: work}
	cmd := NewRootCmd(env)
	cmd.SetContext(context.Background())
	err := runCodeReviewPR(cmd, env, target, flags, seam)
	return out.String(), err
}

// approvePR runs `approve --pr 7` through a seam and returns what it wrote.
func approvePR(t *testing.T, work string, seam clientSeam) (string, error) {
	t.Helper()
	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: work}
	cmd := NewRootCmd(env)
	cmd.SetContext(context.Background())
	err := runCodeReviewApprove(cmd, env, 7, seam)
	return out.String(), err
}

// AGTK_CODE_REVIEW_RELAY is read with surrounding whitespace dropped, and one
// holding only whitespace names no relay.
func TestTheRelayComesFromAgtkCodeReviewRelay(t *testing.T) {
	for _, tc := range []struct {
		vars           map[string]string
		repo, variable string
	}{
		{map[string]string{"AGTK_CODE_REVIEW_RELAY": " acme/relay\n"}, "acme/relay", "AGTK_CODE_REVIEW_RELAY"},
		{map[string]string{"AGTK_CODE_REVIEW_RELAY": " \n"}, "", ""},
		{map[string]string{}, "", ""},
	} {
		repo, variable := clientSeam{getenv: environment(tc.vars)}.relay()
		if repo != tc.repo || variable != tc.variable {
			t.Errorf("%v gave %q from %q, want %q from %q", tc.vars, repo, variable, tc.repo, tc.variable)
		}
	}
}

// A machine holding no registration hands a posting run to the relay it
// names, and posts nothing itself: every request goes to the relay repository,
// under the caller's own token, and the pull request under review is never
// read, fetched or posted to from here.
func TestAnUnregisteredMachineRelaysAPostingRun(t *testing.T) {
	work, _, headSHA := prRepo(t)
	net := &relayNet{conclusion: "success"}
	seam := clientSeam{dir: unregistered(t), doer: net, getenv: environment(relayed())}

	out, err := postPR(t, work, reviewTarget{pr: 7, panel: "deep"}, runFlags{}, seam)
	if err != nil {
		t.Fatalf("a relayed run failed: %v\n%s", err, out)
	}
	for _, want := range []string{"acme/widgets#7", "acme/relay", relayRunURL, "succeeded"} {
		if !strings.Contains(out, want) {
			t.Errorf("the relayed run does not report %q:\n%s", want, out)
		}
	}
	if got := net.relayCalls(); len(got) != len(net.calls) || len(got) != 2 {
		t.Errorf("a relayed run asked for %v, want only the relay's dispatch and its run", net.calls)
	}
	for _, call := range net.calls {
		if !strings.HasSuffix(call, " Bearer ghp_env") {
			t.Errorf("the relay was not reached with the caller's token: %s", call)
		}
	}
	want := map[string]string{"repo": "acme/widgets", "pr": "7", "action": "run", "panel": "deep"}
	got := relayInputs(t, net)
	if len(got) != len(want) {
		t.Errorf("the relay was dispatched with %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("input %s is %q, want %q", k, got[k], v)
		}
	}
	if headFetched(t, work, headSHA) {
		t.Error("a relayed run fetched the head a panel would review, so it read the pull request itself")
	}
}

// A registered machine posts as the App whatever the environment names: the
// relay is an answer to holding no registration, not a second way to post.
func TestARegisteredMachineNeverRelaysWhateverTheEnvironmentHolds(t *testing.T) {
	work, baseSHA, headSHA := prRepo(t)
	dir := registration(t)

	var runs, approvals []string
	for _, vars := range []map[string]string{{}, relayed()} {
		net := &relayNet{conclusion: "success", next: prDoer(baseSHA, headSHA, reviewedBy(headSHA, true), noThreads)}
		out, err := postPR(t, work, reviewTarget{pr: 7}, runFlags{}, clientSeam{dir: dir, doer: net, getenv: environment(vars)})
		if err != nil {
			t.Fatalf("run --pr with %v: %v", vars, err)
		}
		runs = append(runs, out)
		if calls := net.relayCalls(); len(calls) > 0 {
			t.Errorf("run --pr on a registered machine with %v reached the relay: %v", vars, calls)
		}

		marker := reviewrun.ReviewMarker{Head: headSHA, Complete: true}
		net = &relayNet{conclusion: "success", next: &approvalDoer{
			rest: prDoer(baseSHA, headSHA, "", "").rest,
			graphql: []string{
				reviewsAnswer(headSHA, "## Review by `agtk`\n\n"+marker.Render()+"\n"),
				threadsAnswer(),
			},
		}}
		out, err = approvePR(t, work, clientSeam{dir: dir, doer: net, getenv: environment(vars)})
		if err != nil {
			t.Fatalf("approve with %v: %v\n%s", vars, err, out)
		}
		approvals = append(approvals, out)
		if calls := net.relayCalls(); len(calls) > 0 {
			t.Errorf("approve on a registered machine with %v reached the relay: %v", vars, calls)
		}
	}
	if runs[0] != runs[1] {
		t.Errorf("a relay in the environment changed what a registered run reports:\n--- without ---\n%s\n--- with ---\n%s", runs[0], runs[1])
	}
	if approvals[0] != approvals[1] {
		t.Errorf("a relay in the environment changed what a registered approval reports:\n--- without ---\n%s\n--- with ---\n%s",
			approvals[0], approvals[1])
	}
}

// A flag the relay does not carry is refused only where a run would relay. On
// a registered machine --full posts as the App with a relay named, and here
// reaches the no-op on a head that already carries a review.
func TestARegisteredMachineTakesAFlagTheRelayDoesNotCarry(t *testing.T) {
	work, baseSHA, headSHA := prRepo(t)
	net := &relayNet{conclusion: "success", next: prDoer(baseSHA, headSHA, reviewedBy(headSHA, true), noThreads)}
	out, err := postPR(t, work, reviewTarget{pr: 7}, runFlags{full: true},
		clientSeam{dir: registration(t), doer: net, getenv: environment(relayed())})
	if err != nil {
		t.Fatalf("--full on a registered machine with a relay named: %v", err)
	}
	if !strings.Contains(out, "already carries a") {
		t.Errorf("--full on a registered machine did not reach the reviewed head's no-op:\n%s", out)
	}
	if calls := net.relayCalls(); len(calls) > 0 {
		t.Errorf("--full on a registered machine reached the relay: %v", calls)
	}
}

// A registration somebody started and never finished is answered by finishing
// it, whatever relay the environment names. Relaying around it would hide the
// breakage exactly as reading around it with a token would.
func TestAHalfWrittenRegistrationIsNeverRelayedAround(t *testing.T) {
	work, _, _ := prRepo(t)
	dir := registration(t)
	if err := os.Remove(filepath.Join(dir, githubapp.KeyFile)); err != nil {
		t.Fatal(err)
	}
	net := &relayNet{conclusion: "success"}
	seam := clientSeam{dir: dir, doer: net, getenv: environment(relayed())}

	out, err := postPR(t, work, reviewTarget{pr: 7}, runFlags{}, seam)
	if err == nil || !strings.Contains(err.Error(), "register` again") {
		t.Errorf("run --pr relayed around a half-written registration: %v", err)
	}
	approveOut, approveErr := approvePR(t, work, seam)
	if approveErr == nil || !strings.Contains(approveErr.Error(), "register` again") {
		t.Errorf("approve relayed around a half-written registration: %v", approveErr)
	}
	for _, e := range []error{err, approveErr} {
		if e != nil && strings.Contains(e.Error(), "AGTK_CODE_REVIEW_RELAY") {
			t.Errorf("a half-written registration is answered with the relay: %v", e)
		}
	}
	if len(net.calls) > 0 {
		t.Errorf("a half-written registration still sent %v", net.calls)
	}
	if out != "" || approveOut != "" {
		t.Errorf("a refused post still reported something:\n%s%s", out, approveOut)
	}
}

// A relay GitHub will not dispatch — a token without access to it reads the
// same as a relay that is not there — is named with the variable that named
// it and the token that was refused, since those are what somebody changes.
func TestARefusedRelayDispatchNamesTheRelayAndTheToken(t *testing.T) {
	work, _, _ := prRepo(t)
	net := &relayNet{refuse: http.StatusNotFound}
	_, err := postPR(t, work, reviewTarget{pr: 7}, runFlags{},
		clientSeam{dir: unregistered(t), doer: net, getenv: environment(relayed())})
	if err == nil {
		t.Fatal("a refused dispatch was reported as a success")
	}
	for _, want := range []string{"acme/relay", "AGTK_CODE_REVIEW_RELAY", "GH_TOKEN", "404"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
	if len(net.calls) != 1 {
		t.Errorf("a refused dispatch was followed by %v", net.calls)
	}
}

// A relay run that completes without succeeding is a failure, reported with
// how it ended and where its log is, and never as a posted review.
func TestARelayRunThatDidNotSucceedFailsTheCommand(t *testing.T) {
	work, _, _ := prRepo(t)
	net := &relayNet{conclusion: "failure"}
	out, err := postPR(t, work, reviewTarget{pr: 7}, runFlags{},
		clientSeam{dir: unregistered(t), doer: net, getenv: environment(relayed())})
	if err == nil {
		t.Fatalf("a failed relay run was reported as a success:\n%s", out)
	}
	for _, want := range []string{"failure", relayRunURL, "acme/widgets#7"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure does not name %q: %v", want, err)
		}
	}
	if strings.Contains(out, "succeeded") {
		t.Errorf("a failed relay run reported success:\n%s", out)
	}
}

// A relay named with no token to reach it is neither way out, and the refusal
// says which half is missing.
func TestARelayWithNoTokenToReachItIsRefusedBeforeAnythingIsSent(t *testing.T) {
	work, _, _ := prRepo(t)
	net := &relayNet{conclusion: "success"}
	_, err := postPR(t, work, reviewTarget{pr: 7}, runFlags{},
		clientSeam{dir: unregistered(t), doer: net, getenv: environment(map[string]string{"AGTK_CODE_REVIEW_RELAY": "acme/relay"})})
	if err == nil {
		t.Fatal("a relay was dispatched with no token")
	}
	for _, want := range []string{"code-review register", "acme/relay", "GH_TOKEN", "GITHUB_TOKEN"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
	if len(net.calls) > 0 {
		t.Errorf("a relay with no token still sent %v", net.calls)
	}
}

// A relayed run learns how the relay's run ended and nothing of the review
// inside it, so under --json it is refused rather than handing a consumer
// parsing the stream a line of prose.
func TestARelayIsNotUsedUnderJSON(t *testing.T) {
	work, _, _ := prRepo(t)
	net := &relayNet{conclusion: "success"}
	out, err := postPR(t, work, reviewTarget{pr: 7}, runFlags{json: true},
		clientSeam{dir: unregistered(t), doer: net, getenv: environment(relayed())})
	if err == nil || !strings.Contains(err.Error(), "--json") {
		t.Errorf("a relayed run under --json was not refused for it: %v", err)
	}
	if len(net.calls) > 0 || out != "" {
		t.Errorf("a refused relay under --json still sent %v and wrote %q", net.calls, out)
	}
}

// A relay is handed the pull request and its panel alone, so a flag that
// changes what the run posts is refused before anything is sent, rather than
// dropped from a relayed run that would still report success. The refusal
// names the flag, the relay and both ways out.
func TestARelayIsNotUsedUnderAFlagItDoesNotCarry(t *testing.T) {
	for _, tc := range []struct {
		flags        runFlags
		named, unset []string
	}{
		{runFlags{force: true}, []string{"--force"}, []string{"--full"}},
		{runFlags{full: true}, []string{"--full"}, []string{"--force"}},
		{runFlags{force: true, full: true}, []string{"--force and --full"}, nil},
	} {
		work, _, _ := prRepo(t)
		net := &relayNet{conclusion: "success"}
		out, err := postPR(t, work, reviewTarget{pr: 7, panel: "deep"}, tc.flags,
			clientSeam{dir: unregistered(t), doer: net, getenv: environment(relayed())})
		if err == nil {
			t.Fatalf("%+v was relayed without what it asks for:\n%s", tc.flags, out)
		}
		for _, want := range append([]string{"acme/relay", "AGTK_CODE_REVIEW_RELAY", "code-review register"}, tc.named...) {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal of %+v does not name %q: %v", tc.flags, want, err)
			}
		}
		for _, unwanted := range tc.unset {
			if strings.Contains(err.Error(), unwanted) {
				t.Errorf("the refusal of %+v names %s, which was not passed: %v", tc.flags, unwanted, err)
			}
		}
		if len(net.calls) > 0 || out != "" {
			t.Errorf("a refused relay under %+v still sent %v and wrote %q", tc.flags, net.calls, out)
		}
	}
}

// A run that posts nothing has no post to relay. --dry-run and --no-post read
// through the token as they do with no relay named, and without a token they
// are refused as they are with no relay named, whether or not a flag the
// relay does not carry is also set.
func TestARunThatPostsNothingNeverRelays(t *testing.T) {
	for _, flags := range []runFlags{
		{dryRun: true}, {noPost: true},
		{dryRun: true, force: true, full: true}, {noPost: true, force: true, full: true},
	} {
		work, _, _ := prRepo(t)
		net := &relayNet{conclusion: "success"}
		_, err := postPR(t, work, reviewTarget{pr: 7}, flags, clientSeam{
			dir: unregistered(t), doer: net,
			getenv: environment(map[string]string{"AGTK_CODE_REVIEW_RELAY": "acme/relay"}),
		})
		if err == nil || !strings.Contains(err.Error(), "GH_TOKEN or GITHUB_TOKEN to a token that can read") {
			t.Errorf("%+v with no token was not refused as a read: %v", flags, err)
		}
		if err != nil && strings.Contains(err.Error(), "AGTK_CODE_REVIEW_RELAY") {
			t.Errorf("%+v, which posts nothing, offered the relay: %v", flags, err)
		}
		if len(net.calls) > 0 {
			t.Errorf("%+v with no token still sent %v", flags, net.calls)
		}
	}

	for _, flags := range []runFlags{{dryRun: true}, {dryRun: true, force: true, full: true}} {
		work, baseSHA, headSHA := prRepo(t)
		net := &relayNet{conclusion: "success", next: prDoer(baseSHA, headSHA, reviewsOf(), noThreads)}
		out, err := postPR(t, work, reviewTarget{pr: 7}, flags,
			clientSeam{dir: unregistered(t), doer: net, getenv: environment(relayed())})
		if err != nil {
			t.Fatalf("%+v with a token and a relay: %v", flags, err)
		}
		if !strings.Contains(out, "Nothing was spent and nothing was posted.") {
			t.Errorf("%+v did not preview the review:\n%s", flags, out)
		}
		if calls := net.relayCalls(); len(calls) > 0 {
			t.Errorf("%+v reached the relay: %v", flags, calls)
		}
		if len(net.calls) == 0 {
			t.Errorf("%+v read nothing with the token", flags)
		}
	}
}
