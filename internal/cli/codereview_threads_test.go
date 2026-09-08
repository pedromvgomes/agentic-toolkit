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
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewpost"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun"
)

// graphQLDoer answers REST by path and GraphQL by which query was asked, since
// every GraphQL call is a POST to one path.
type graphQLDoer struct {
	rest stubDoer
	// reviews and threads are the answers to the two queries, and an empty
	// one means that query is refused the way GraphQL refuses: a 200 carrying
	// an errors array.
	reviews string
	threads string
	// asked records which queries were made, so a test can assert that a
	// no-op stopped at the one query that decided it.
	asked []string
}

func (d *graphQLDoer) Do(req *http.Request) (*http.Response, error) {
	if req.URL.Path != "/graphql" {
		return d.rest.Do(req)
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	body := d.threads
	name := "threads"
	if strings.Contains(payload.Query, "reviews(") {
		body, name = d.reviews, "reviews"
	}
	d.asked = append(d.asked, name)
	if body == "" {
		body = `{"data":null,"errors":[{"type":"FORBIDDEN","message":"Resource not accessible by integration"}]}`
	}
	return &http.Response{
		StatusCode: 200, Header: http.Header{},
		Body: io.NopCloser(strings.NewReader(body)),
	}, nil
}

// reviewedBy renders the reviews query's answer for one commit.
func reviewedBy(oid string, byViewer bool) string {
	return fmt.Sprintf(`{"data":{"repository":{"pullRequest":{"reviews":{"pageInfo":{"hasNextPage":false,"endCursor":""},`+
		`"nodes":[{"commit":{"oid":%q},"viewerDidAuthor":%v}]}}}}}`, oid, byViewer)
}

// noThreads is a pull request carrying no comment threads.
const noThreads = `{"data":{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[]}}}}}`

// prDoer builds a transport that answers everything a pull request review
// reads, with the two GraphQL answers the caller chooses.
func prDoer(baseSHA, headSHA, reviews, threads string) *graphQLDoer {
	return &graphQLDoer{
		rest: stubDoer{
			"/repos/acme/widgets/installation":    `{"id": 99}`,
			"/app/installations/99/access_tokens": `{"token": "ghs_x", "expires_at": "2999-01-01T00:00:00Z"}`,
			"/repos/acme/widgets/pulls/7": fmt.Sprintf(
				`{"number": 7, "state": "open", "base": {"sha": %q, "ref": "main"}, "head": {"sha": %q, "ref": "feature/x"}}`,
				baseSHA, headSHA),
		},
		reviews: reviews,
		threads: threads,
	}
}

// runPR drives a pull request review with PATH holding no provider, so a run
// that got as far as starting a panel fails loudly rather than silently
// spending nothing.
func runPR(t *testing.T, work string, flags runFlags, doer *graphQLDoer) (string, error) {
	t.Helper()
	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: work}
	cmd := NewRootCmd(env)
	cmd.SetContext(context.Background())
	err := runCodeReviewPR(cmd, env, reviewTarget{pr: 7}, flags, clientSeam{dir: registration(t), doer: doer})
	return out.String(), err
}

// Re-reviewing a commit spends a panel to re-derive what the pull request is
// already displaying. The check is read before a reviewer starts, so the no-op
// costs one query rather than a panel.
func TestAHeadThatAlreadyCarriesAReviewIsANoOpThatNamesTheCommit(t *testing.T) {
	work, baseSHA, headSHA := prRepo(t)
	doer := prDoer(baseSHA, headSHA, reviewedBy(headSHA, true), noThreads)

	body, err := runPR(t, work, runFlags{}, doer)
	if err != nil {
		t.Fatalf("the no-op failed: %v", err)
	}
	if !strings.Contains(body, headSHA) {
		t.Errorf("the no-op does not name the commit:\n%s", body)
	}
	for _, want := range []string{"already carries a review", "nothing was spent", "--force"} {
		if !strings.Contains(body, want) {
			t.Errorf("the no-op does not say %q:\n%s", want, body)
		}
	}
	// Nothing beyond the reviews query was read: the threads are only needed
	// by a run that is going to post.
	if len(doer.asked) != 1 || doer.asked[0] != "reviews" {
		t.Errorf("a no-op made these queries: %v", doer.asked)
	}
}

// A review somebody else posted says nothing about whether this App has
// reviewed the head, and stopping on one would make the command refuse to run
// on any pull request a person has reviewed.
func TestSomebodyElsesReviewOfTheHeadIsNotThisAppsReview(t *testing.T) {
	work, baseSHA, headSHA := prRepo(t)
	doer := prDoer(baseSHA, headSHA, reviewedBy(headSHA, false), noThreads)

	body, _ := runPR(t, work, runFlags{}, doer)
	if strings.Contains(body, "already carries a review") {
		t.Errorf("somebody else's review stopped the run:\n%s", body)
	}
}

// A review bound to another commit is a review of code this run is not looking
// at, which is the whole reason a review is bound to a commit.
func TestAReviewOfAnEarlierCommitDoesNotStopTheRun(t *testing.T) {
	work, baseSHA, headSHA := prRepo(t)
	doer := prDoer(baseSHA, headSHA, reviewedBy(baseSHA, true), noThreads)

	body, _ := runPR(t, work, runFlags{}, doer)
	if strings.Contains(body, "already carries a review") {
		t.Errorf("a review of an earlier commit stopped the run:\n%s", body)
	}
}

// --force is the operator saying they want this commit reviewed again.
func TestForceReviewsAHeadThatAlreadyCarriesAReview(t *testing.T) {
	work, baseSHA, headSHA := prRepo(t)
	doer := prDoer(baseSHA, headSHA, reviewedBy(headSHA, true), noThreads)

	body, _ := runPR(t, work, runFlags{force: true}, doer)
	if strings.Contains(body, "already carries a review") {
		t.Errorf("--force stopped at the head it was passed to review:\n%s", body)
	}
	// And it went on to read what the pull request carries, because --force
	// overrides the no-op and not the withholding.
	if len(doer.asked) != 2 {
		t.Errorf("--force made these queries: %v", doer.asked)
	}
}

// The preview spends nothing, so there is nothing for the no-op to save and
// the plan is what was asked for.
func TestAPreviewIsNotStoppedByAHeadThatCarriesAReview(t *testing.T) {
	work, baseSHA, headSHA := prRepo(t)
	doer := prDoer(baseSHA, headSHA, reviewedBy(headSHA, true), noThreads)

	body, err := runPR(t, work, runFlags{dryRun: true}, doer)
	if err != nil {
		t.Fatalf("the preview failed: %v", err)
	}
	if strings.Contains(body, "already carries a review") {
		t.Errorf("a preview was stopped by a review it was not going to duplicate:\n%s", body)
	}
	if !strings.Contains(body, "would be made, and nothing was spent") {
		t.Errorf("the preview did not print a plan:\n%s", body)
	}
}

// A thread query that failed and a pull request carrying nothing both end in
// "nothing was withheld", and only one of them means the pull request is
// clean.
func TestAFailedThreadReadIsReportedRatherThanReadAsAnEmptyPullRequest(t *testing.T) {
	work, baseSHA, headSHA := prRepo(t)
	// The reviews query answers and the threads query is refused, so the run
	// gets past the no-op and reaches the read that fails.
	doer := prDoer(baseSHA, headSHA, reviewedBy(baseSHA, true), "")

	body, _ := runPR(t, work, runFlags{dryRun: true}, doer)
	if !strings.Contains(body, "FORBIDDEN") {
		t.Errorf("a refused thread query left no trace:\n%s", body)
	}
}

// A run whose thread read failed has to say so where the review is read, not
// only where the query was made.
func TestARenderedReviewSaysWhenItsThreadsCouldNotBeRead(t *testing.T) {
	r := reviewFor(true)
	r.Threads = reviewrun.ThreadsUnreadable("GitHub refused the review threads query: FORBIDDEN")
	var out bytes.Buffer
	reviewrun.Render(&out, r)
	if !strings.Contains(out.String(), "Could not read the existing threads") {
		t.Errorf("the rendered review does not report the failed read:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "may repeat") {
		t.Errorf("the rendered review does not say what that costs:\n%s", out.String())
	}
}

// A --json consumer branching on an empty `suppressed` list cannot tell a pull
// request with nothing to withhold from a read that never arrived, so
// availability travels with the counts.
func TestThePostedJSONSeparatesAFailedThreadReadFromAQuietPullRequest(t *testing.T) {
	target := &pullRequestTarget{
		slug: mustSlug("acme", "widgets"),
		pr:   githubapp.PullRequest{Number: 7, HeadSHA: "abc", BaseSHA: "def"},
	}

	unread := reviewFor(true)
	unread.Threads = reviewrun.ThreadsUnreadable("GitHub refused the review threads query: FORBIDDEN")
	quiet := reviewFor(true)
	quiet.Threads = reviewrun.ThreadsRead(nil)

	decode := func(r *reviewrun.Review) map[string]any {
		t.Helper()
		raw, err := json.Marshal(pullRequestPostJSON(target, r, githubapp.ReviewPayload{}, reviewpost.Placement{}, nil, nil))
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		threads, ok := out["threads"].(map[string]any)
		if !ok {
			t.Fatalf("the posted JSON carries no threads object: %s", raw)
		}
		return threads
	}

	failed, clean := decode(unread), decode(quiet)
	if failed["available"] != false {
		t.Errorf("a failed read reports available %v", failed["available"])
	}
	if !strings.Contains(failed["reason"].(string), "FORBIDDEN") {
		t.Errorf("a failed read does not carry why: %v", failed["reason"])
	}
	if clean["available"] != true {
		t.Errorf("a pull request with no threads reports available %v", clean["available"])
	}
	if _, named := clean["reason"]; named {
		t.Errorf("a read that answered carries a reason: %v", clean["reason"])
	}
	// An empty collection serialises as a list, so a consumer never has to
	// tell absent from empty.
	for _, threads := range []map[string]any{failed, clean} {
		withheld, ok := threads["suppressed"].([]any)
		if !ok || withheld == nil {
			t.Errorf("suppressed is not an empty list: %#v", threads["suppressed"])
		}
	}
}

// A no-op posts nothing and runs nothing, and a consumer has to be able to
// tell that from a review that ran and found nothing to say.
func TestTheNoOpJSONSaysNoPanelRan(t *testing.T) {
	work, baseSHA, headSHA := prRepo(t)
	doer := prDoer(baseSHA, headSHA, reviewedBy(headSHA, true), noThreads)

	body, err := runPR(t, work, runFlags{json: true}, doer)
	if err != nil {
		t.Fatalf("the no-op failed: %v", err)
	}
	var out struct {
		Version     int  `json:"version"`
		Posted      bool `json:"posted"`
		Ran         bool `json:"ran"`
		Reason      string
		PullRequest struct {
			HeadSHA string `json:"head_sha"`
			Number  int    `json:"number"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("the no-op did not emit JSON: %v\n%s", err, body)
	}
	if out.Ran || out.Posted {
		t.Errorf("the no-op reports ran=%v posted=%v", out.Ran, out.Posted)
	}
	if out.PullRequest.HeadSHA != headSHA || out.PullRequest.Number != 7 {
		t.Errorf("the no-op does not name the commit it skipped: %+v", out.PullRequest)
	}
	if !strings.Contains(out.Reason, "--force") {
		t.Errorf("the no-op does not say how to review it anyway: %q", out.Reason)
	}
}

// --force overrides a check only a pull request has. Silently ignoring it
// would read as a flag that was honoured.
func TestForceWithoutAPullRequestIsRefused(t *testing.T) {
	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: t.TempDir()}
	cmd := NewRootCmd(env)
	cmd.SetContext(context.Background())
	err := runCodeReviewRun(cmd, env, reviewTarget{context: "worktree"}, runFlags{force: true})
	if err == nil {
		t.Fatal("--force was accepted without --pr")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("the refusal does not name the flag: %v", err)
	}
}
