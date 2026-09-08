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
