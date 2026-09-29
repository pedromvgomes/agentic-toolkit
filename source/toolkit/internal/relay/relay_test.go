package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

// exchange is one scripted request and the answer it gets.
type exchange struct {
	method string
	path   string
	status int
	body   string
	header http.Header
}

// scripted is the network, replaced. Each request is matched against the next
// unconsumed exchange, so a call made out of order, or one too many, fails
// rather than passing on a coincidence.
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

// done reports that every scripted exchange was used.
func (s *scripted) done() {
	s.t.Helper()
	if len(s.seen) != len(s.exchanges) {
		s.t.Errorf("%d of %d scripted calls were made", len(s.seen), len(s.exchanges))
	}
}

const dispatchPath = "/repos/acme/relay/actions/workflows/relay.yml/dispatches"

var target = Target{Slug: "acme/relay", Token: "ghp_caller"}

// review is a run request's payload. Opaque to this package, so any non-empty
// JSON stands in for the review the caller computed.
const review = `{"review":{"commit_id":"abc","body":"ok","event":"COMMENT","comments":[]},"file_comments":[]}`

// dispatchBody is the body a dispatch sent, decoded.
type dispatchBody struct {
	Ref              string            `json:"ref"`
	Inputs           map[string]string `json:"inputs"`
	ReturnRunDetails bool              `json:"return_run_details"`
}

func decodeDispatch(t *testing.T, raw string) dispatchBody {
	t.Helper()
	var body dispatchBody
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("the dispatch body is not JSON: %v: %s", err, raw)
	}
	return body
}

// The relay is read against the schema the rest of the binary reads GitHub
// with. A pin that drifted would let one field mean two things in one process.
func TestTheRelayPinsTheAPIVersionTheBinaryUses(t *testing.T) {
	if apiVersion != githubapp.APIVersion {
		t.Errorf("the relay pins API version %s and githubapp pins %s", apiVersion, githubapp.APIVersion)
	}
}

func TestADispatchStartsTheRelayWorkflowOnMain(t *testing.T) {
	net := &scripted{t: t, exchanges: []exchange{
		{method: http.MethodPost, path: dispatchPath, status: http.StatusNoContent},
	}}
	before := time.Now()
	d, err := Dispatch(context.Background(), net, target,
		Request{Repo: "acme/widgets", PR: 42, Action: ActionRun, Payload: review})
	if err != nil {
		t.Fatal(err)
	}
	net.done()

	req := net.seen[0]
	for header, want := range map[string]string{
		"Authorization":        "Bearer ghp_caller",
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": githubapp.APIVersion,
		"User-Agent":           userAgent,
		"Content-Type":         "application/json",
	} {
		if got := req.Header.Get(header); got != want {
			t.Errorf("%s is %q, want %q", header, got, want)
		}
	}
	if req.URL.Host != "api.github.com" {
		t.Errorf("the dispatch went to %s, want api.github.com", req.URL.Host)
	}

	body := decodeDispatch(t, net.bodies[0])
	if body.Ref != "main" {
		t.Errorf("ref is %q, want main", body.Ref)
	}
	if !body.ReturnRunDetails {
		t.Error("the dispatch does not ask GitHub to name the run it starts")
	}
	want := map[string]string{"repo": "acme/widgets", "pr": "42", "action": "run", "payload": review}
	if len(body.Inputs) != len(want) {
		t.Errorf("inputs are %v, want %v", body.Inputs, want)
	}
	for k, v := range want {
		if body.Inputs[k] != v {
			t.Errorf("input %s is %q, want %q", k, body.Inputs[k], v)
		}
	}
	if _, ok := body.Inputs["panel"]; ok {
		t.Errorf("a dispatch with no panel sent one: %s", net.bodies[0])
	}

	if d.RunID != 0 || d.URL != "" {
		t.Errorf("a 204 named run %d at %q", d.RunID, d.URL)
	}
	if d.Since.Before(before) || d.Since.After(time.Now()) {
		t.Errorf("since is %v, not the time of the dispatch", d.Since)
	}
	if d.Request.Repo != "acme/widgets" || d.Request.PR != 42 {
		t.Errorf("the dispatch lost its request: %+v", d.Request)
	}
}

func TestADispatchCarriesAPanelWhenOneIsGiven(t *testing.T) {
	net := &scripted{t: t, exchanges: []exchange{
		{method: http.MethodPost, path: dispatchPath, status: http.StatusNoContent},
	}}
	_, err := Dispatch(context.Background(), net, target,
		Request{Repo: "acme/widgets", PR: 42, Action: ActionRun, Panel: "security", Payload: review})
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeDispatch(t, net.bodies[0]).Inputs["panel"]; got != "security" {
		t.Errorf("panel is %q, want security", got)
	}
}

func TestADispatchKeepsTheRunGitHubNames(t *testing.T) {
	net := &scripted{t: t, exchanges: []exchange{
		{method: http.MethodPost, path: dispatchPath, status: http.StatusOK,
			body: `{"workflow_run_id": 777, "run_url": "https://api.github.com/repos/acme/relay/actions/runs/777",
				"html_url": "https://github.com/acme/relay/actions/runs/777"}`},
	}}
	d, err := Dispatch(context.Background(), net, target,
		Request{Repo: "acme/widgets", PR: 42, Action: ActionApprove})
	if err != nil {
		t.Fatal(err)
	}
	if d.RunID != 777 || d.URL != "https://github.com/acme/relay/actions/runs/777" {
		t.Errorf("the dispatch named run %d at %q, want 777 and its page", d.RunID, d.URL)
	}
	if _, ok := decodeDispatch(t, net.bodies[0]).Inputs["payload"]; ok {
		t.Errorf("an approval was dispatched with a payload: %s", net.bodies[0])
	}
}

func TestARefusedDispatchSaysWhatGitHubSaid(t *testing.T) {
	for _, tc := range []struct {
		status  int
		body    string
		message string
	}{
		{http.StatusNotFound, `{"message": "Not Found"}`, "Not Found"},
		{http.StatusUnprocessableEntity, `{"message": "Unexpected inputs provided: [\"panel\"]"}`, "Unexpected inputs provided"},
		{http.StatusBadGateway, `upstream unavailable`, "upstream unavailable"},
	} {
		net := &scripted{t: t, exchanges: []exchange{
			{method: http.MethodPost, path: dispatchPath, status: tc.status, body: tc.body},
		}}
		_, err := Dispatch(context.Background(), net, target,
			Request{Repo: "acme/widgets", PR: 42, Action: ActionRun, Payload: review})
		var apiErr *Error
		if !errors.As(err, &apiErr) {
			t.Fatalf("a %d came back as %v, not a GitHub refusal", tc.status, err)
		}
		if apiErr.StatusCode != tc.status || !strings.Contains(err.Error(), tc.message) {
			t.Errorf("a %d reads %q, want it to carry %q", tc.status, err, tc.message)
		}
		if !strings.Contains(err.Error(), "acme/relay") {
			t.Errorf("%q does not name the relay it was refused by", err)
		}
	}
}

// The boundary itself, 300, is where a response stops being one this package
// reads a body out of and starts being one it reports as a refusal.
func TestCallTreatsThreeHundredAsARefusal(t *testing.T) {
	net := &scripted{t: t, exchanges: []exchange{
		{method: http.MethodPost, path: dispatchPath, status: 300, body: `{"message": "Multiple Choices"}`},
	}}
	_, err := Dispatch(context.Background(), net, target,
		Request{Repo: "acme/widgets", PR: 42, Action: ActionRun, Payload: review})
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 300 {
		t.Fatalf("a 300 came back as %v, want it read as a refusal", err)
	}
}

// apiMessage prefers GitHub's own field, and falls back to the body itself
// both when the body is not that shape and when the field it names is empty.
func TestAPIMessagePrefersGitHubsFieldAndFallsBackToTheBody(t *testing.T) {
	if got := apiMessage([]byte(`{"message": "Not Found"}`)); got != "Not Found" {
		t.Errorf("apiMessage read %q, want GitHub's own message", got)
	}
	if got := apiMessage([]byte(`not json`)); got != "not json" {
		t.Errorf("apiMessage of a non-JSON body read %q, want the body itself", got)
	}
	if got := apiMessage([]byte(`{"message": ""}`)); got != `{"message": ""}` {
		t.Errorf("apiMessage of an empty message read %q, want the body itself", got)
	}
}

// Retry-After is seconds, not nanoseconds: a header of "5" reset close to 5
// seconds out, not a duration so small it never changes the wait.
func TestRateLimitResetReadsRetryAfterInSeconds(t *testing.T) {
	res := &http.Response{Header: http.Header{"Retry-After": []string{"5"}}}
	got := rateLimitReset(res)
	if d := time.Until(got); d < 4*time.Second || d > 6*time.Second {
		t.Errorf("Retry-After: 5 reset in %s, want close to 5s", d)
	}
}

// X-RateLimit-Reset is read only once the primary limit is actually
// exhausted; otherwise it is a future timestamp this package must not treat
// as a reason to wait.
func TestRateLimitResetIgnoresXRateLimitHeadersUnlessRemainingIsZero(t *testing.T) {
	future := time.Now().Add(time.Hour).Unix()

	withRemaining := &http.Response{Header: http.Header{}}
	withRemaining.Header.Set("X-RateLimit-Remaining", "1")
	withRemaining.Header.Set("X-RateLimit-Reset", fmt.Sprintf("%d", future))
	if got := rateLimitReset(withRemaining); !got.IsZero() {
		t.Errorf("a response with remaining requests reset at %s, want zero", got)
	}

	exhausted := &http.Response{Header: http.Header{}}
	exhausted.Header.Set("X-RateLimit-Remaining", "0")
	exhausted.Header.Set("X-RateLimit-Reset", fmt.Sprintf("%d", future))
	if got := rateLimitReset(exhausted); got.Unix() != future {
		t.Errorf("an exhausted primary limit reset at %d, want %d", got.Unix(), future)
	}
}

// Every one of these would reach the relay as a run that fails on its own
// inputs, or as a path that addresses something other than the relay's
// workflow, so none is sent.
func TestAMalformedDispatchIsNeverSent(t *testing.T) {
	good := Request{Repo: "acme/widgets", PR: 42, Action: ActionRun, Payload: review}
	for name, tc := range map[string]struct {
		target Target
		req    Request
	}{
		"no token":               {Target{Slug: "acme/relay", Token: "  "}, good},
		"relay not owner/name":   {Target{Slug: "acme", Token: "t"}, good},
		"relay with a path":      {Target{Slug: "acme/relay/../other", Token: "t"}, good},
		"relay with a query":     {Target{Slug: "acme/relay?x=1", Token: "t"}, good},
		"repo not owner/name":    {target, Request{Repo: "widgets", PR: 42, Action: ActionRun, Payload: review}},
		"no pull request":        {target, Request{Repo: "acme/widgets", Action: ActionRun, Payload: review}},
		"unknown action":         {target, Request{Repo: "acme/widgets", PR: 42, Action: "merge"}},
		"panel on an approval":   {target, Request{Repo: "acme/widgets", PR: 42, Action: ActionApprove, Panel: "security"}},
		"run with no payload":    {target, Request{Repo: "acme/widgets", PR: 42, Action: ActionRun}},
		"run with a blank one":   {target, Request{Repo: "acme/widgets", PR: 42, Action: ActionRun, Payload: " \n"}},
		"payload on an approval": {target, Request{Repo: "acme/widgets", PR: 42, Action: ActionApprove, Payload: review}},
	} {
		net := &scripted{t: t}
		if _, err := Dispatch(context.Background(), net, tc.target, tc.req); err == nil {
			t.Errorf("%s: dispatched", name)
		}
		if _, err := Await(context.Background(), net, tc.target, Dispatched{Request: tc.req}, time.Second); err == nil {
			t.Errorf("%s: awaited", name)
		}
	}
}

// A run hands the relay the review it posts, and an approval hands it nothing
// but the pull request: a run without a payload would have the relay post
// nothing and report success, and a payload on an approval is one the relay
// never reads. Each refusal says which.
func TestARequestCarriesAPayloadExactlyWhenItIsARun(t *testing.T) {
	for _, tc := range []struct {
		req  Request
		want string
	}{
		{Request{Repo: "acme/widgets", PR: 42, Action: ActionRun}, "carries none"},
		{Request{Repo: "acme/widgets", PR: 42, Action: ActionApprove, Payload: review}, "takes no payload"},
	} {
		err := tc.req.check()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v was checked as %v, want a refusal saying %q", tc.req, err, tc.want)
		}
	}
	for _, ok := range []Request{
		{Repo: "acme/widgets", PR: 42, Action: ActionRun, Payload: review},
		{Repo: "acme/widgets", PR: 42, Action: ActionApprove},
	} {
		if err := ok.check(); err != nil {
			t.Errorf("%+v was refused: %v", ok, err)
		}
	}
}
