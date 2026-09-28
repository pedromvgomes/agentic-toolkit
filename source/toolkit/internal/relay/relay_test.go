package relay

import (
	"context"
	"encoding/json"
	"errors"
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
		Request{Repo: "acme/widgets", PR: 42, Action: ActionRun})
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
	want := map[string]string{"repo": "acme/widgets", "pr": "42", "action": "run"}
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
		Request{Repo: "acme/widgets", PR: 42, Action: ActionRun, Panel: "security"})
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
			Request{Repo: "acme/widgets", PR: 42, Action: ActionRun})
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

// Every one of these would reach the relay as a run that fails on its own
// inputs, or as a path that addresses something other than the relay's
// workflow, so none is sent.
func TestAMalformedDispatchIsNeverSent(t *testing.T) {
	good := Request{Repo: "acme/widgets", PR: 42, Action: ActionRun}
	for name, tc := range map[string]struct {
		target Target
		req    Request
	}{
		"no token":             {Target{Slug: "acme/relay", Token: "  "}, good},
		"relay not owner/name": {Target{Slug: "acme", Token: "t"}, good},
		"relay with a path":    {Target{Slug: "acme/relay/../other", Token: "t"}, good},
		"relay with a query":   {Target{Slug: "acme/relay?x=1", Token: "t"}, good},
		"repo not owner/name":  {target, Request{Repo: "widgets", PR: 42, Action: ActionRun}},
		"no pull request":      {target, Request{Repo: "acme/widgets", Action: ActionRun}},
		"unknown action":       {target, Request{Repo: "acme/widgets", PR: 42, Action: "merge"}},
		"panel on an approval": {target, Request{Repo: "acme/widgets", PR: 42, Action: ActionApprove, Panel: "security"}},
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
