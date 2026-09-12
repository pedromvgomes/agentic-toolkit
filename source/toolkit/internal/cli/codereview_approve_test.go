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

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewapprove"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun"
)

// approvalDoer answers the REST calls by path and the GraphQL calls in the
// order they are made.
//
// GraphQL serves every query from one path, and approval makes two different
// ones, so a map keyed by path cannot describe what this command does. The
// order is part of what is under test: the reviews are read before the
// threads, and the approval is posted after both.
type approvalDoer struct {
	rest    stubDoer
	graphql []string
	// posted is the body of the review that was posted, and is empty when
	// none was.
	posted string
}

func (d *approvalDoer) Do(req *http.Request) (*http.Response, error) {
	if req.URL.Path == "/graphql" {
		if len(d.graphql) == 0 {
			return &http.Response{StatusCode: 500, Header: http.Header{},
				Body: io.NopCloser(strings.NewReader(`{"message": "an unscripted query"}`))}, nil
		}
		body := d.graphql[0]
		d.graphql = d.graphql[1:]
		return &http.Response{StatusCode: 200, Header: http.Header{},
			Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/pulls/7/reviews") {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		d.posted = string(raw)
		return &http.Response{StatusCode: 200, Header: http.Header{},
			Body: io.NopCloser(strings.NewReader(`{"id": 1, "html_url": "https://github.test/r/1", "state": "APPROVED"}`))}, nil
	}
	return d.rest.Do(req)
}

// reviewsAnswer renders the reviews query's answer for one review this
// installation posted against head.
func reviewsAnswer(head, body string) string {
	encoded, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return `{"data":{"repository":{"pullRequest":{"reviews":{` +
		`"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[` +
		`{"body":` + string(encoded) + `,"commit":{"oid":"` + head + `"},"viewerDidAuthor":true}]}}}}}`
}

// threadsAnswer renders the answered-threads query's answer for the nodes
// given.
func threadsAnswer(nodes ...string) string {
	return `{"data":{"repository":{"pullRequest":{"reviewThreads":{` +
		`"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[` +
		strings.Join(nodes, ",") + `]}}}}}`
}

// agtkThread renders one thread this installation opened for a finding, with
// the replies given.
func agtkThread(fingerprint string, resolved bool, replies ...string) string {
	root := `{"body":` + quoted("off by one\n\n"+reviewrun.FingerprintMarker(fingerprint)) +
		`,"viewerDidAuthor":true,"authorAssociation":"NONE"}`
	comments := append([]string{root}, replies...)
	return `{"path":"a.go","isResolved":` + boolJSON(resolved) +
		`,"isOutdated":false,"comments":{"pageInfo":{"hasNextPage":false},"nodes":[` +
		strings.Join(comments, ",") + `]}}`
}

// reply renders one reply on a thread.
func reply(body, association string) string {
	return `{"body":` + quoted(body) + `,"viewerDidAuthor":false,"authorAssociation":"` + association + `"}`
}

func quoted(s string) string {
	encoded, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func boolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// approvalRun points the command at a pull request whose head is the one this
// repository is at, and returns what it wrote and what it returned.
func approvalRun(t *testing.T, marker reviewrun.ReviewMarker, threads ...string) (string, *approvalDoer, error) {
	t.Helper()
	work, baseSHA, headSHA := prRepo(t)
	marker.Head = headSHA
	doer := &approvalDoer{
		rest: stubDoer{
			"/repos/acme/widgets/installation":    `{"id": 99}`,
			"/app/installations/99/access_tokens": `{"token": "ghs_x", "expires_at": "2999-01-01T00:00:00Z"}`,
			"/repos/acme/widgets/pulls/7": fmt.Sprintf(
				`{"number": 7, "state": "open", "base": {"sha": %q, "ref": "main"}, "head": {"sha": %q, "ref": "feature/x"}}`,
				baseSHA, headSHA),
		},
		graphql: []string{
			reviewsAnswer(headSHA, "## Review by `agtk`\n\n"+marker.Render()+"\n"),
			threadsAnswer(threads...),
		},
	}

	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: work}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	err := runCodeReviewApprove(cmd, env, 7, clientSeam{dir: registration(t), doer: doer})
	return out.String(), doer, err
}

// answerable is a finding the review gave a thread to.
func answerable(fingerprint string, severity review.Severity) reviewrun.MarkedFinding {
	return reviewrun.MarkedFinding{Fingerprint: fingerprint, Severity: severity, Answerable: true}
}

// The whole point of the subcommand: a solo author cannot approve their own
// pull request, and the App's review is what satisfies the requirement.
func TestApprovingAReviewedHeadPostsOneApprovalBoundToIt(t *testing.T) {
	marker := reviewrun.ReviewMarker{Complete: true, Findings: []reviewrun.MarkedFinding{
		answerable("aaaaaaaaaaaa", review.SeverityRed),
	}}
	out, doer, err := approvalRun(t, marker,
		agtkThread("aaaaaaaaaaaa", true, reply(reviewapprove.Marking+": guarded upstream", "OWNER")))
	if err != nil {
		t.Fatalf("approve: %v\n%s", err, out)
	}
	if doer.posted == "" {
		t.Fatal("nothing was posted")
	}
	// What event it carries is internal/reviewapprove's own test to make: the
	// literal is named in one package, and a guard holds it there.
	var payload struct {
		CommitID string `json:"commit_id"`
	}
	if err := json.Unmarshal([]byte(doer.posted), &payload); err != nil {
		t.Fatalf("the posted body is not JSON: %v\n%s", err, doer.posted)
	}
	if payload.CommitID == "" {
		t.Error("the approval is bound to no commit, so it describes whatever the head is when it lands")
	}
	if !strings.Contains(out, payload.CommitID) {
		t.Errorf("the terminal does not say which commit was approved:\n%s", out)
	}
	// Push access is re-evaluated when the pull request is read, not when the
	// review was submitted, so a successful post is not evidence that the
	// approval counts — ADR 0009.
	if !strings.Contains(out, "while the App can push") {
		t.Errorf("a successful post is presented as an approval that counts:\n%s", out)
	}
}

// A refusal is a list of acts somebody has to perform. Naming the state
// without naming the act leaves them to guess it.
func TestARefusedApprovalNamesEveryConditionAndWhatWouldAnswerIt(t *testing.T) {
	marker := reviewrun.ReviewMarker{Complete: false, Findings: []reviewrun.MarkedFinding{
		answerable("aaaaaaaaaaaa", review.SeverityRed),
	}}
	out, doer, err := approvalRun(t, marker, agtkThread("aaaaaaaaaaaa", false))
	if err == nil {
		t.Fatal("a head meeting none of the conditions was approved")
	}
	if doer.posted != "" {
		t.Errorf("a refused approval posted %s", doer.posted)
	}
	for _, want := range []string{
		"did not reach a verdict",
		"aaaaaaaaaaaa",
		reviewapprove.Marking,
		"unresolved",
		"There is no flag that approves anyway.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal does not carry %q:\n%s", want, out)
		}
	}
}

// The deadlock is the one refusal with no act behind it, and saying "nothing
// here will help" is the honest report.
func TestADeadlockedApprovalOffersNoWayPast(t *testing.T) {
	marker := reviewrun.ReviewMarker{Complete: true, Findings: []reviewrun.MarkedFinding{
		{Fingerprint: "dddddddddddd", Severity: review.SeverityRed, Injected: true},
	}}
	out, _, err := approvalRun(t, marker)
	if err == nil {
		t.Fatal("a pull request carrying an unattachable injection finding was approved")
	}
	if !strings.Contains(out, "removing the text from the change") {
		t.Errorf("the deadlock does not say what the only remedy is:\n%s", out)
	}
}
