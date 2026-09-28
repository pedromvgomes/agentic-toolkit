package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	listPath = "/repos/acme/relay/actions/workflows/relay.yml/runs"
	fastPoll = time.Millisecond
)

func runPath(id int64) string { return fmt.Sprintf("/repos/acme/relay/actions/runs/%d", id) }

func runURL(id int64) string { return fmt.Sprintf("https://github.com/acme/relay/actions/runs/%d", id) }

// runJSON is one run as GitHub reports it.
func runJSON(id int64, status, conclusion, title string, created time.Time) string {
	c := "null"
	if conclusion != "" {
		c = fmt.Sprintf("%q", conclusion)
	}
	return fmt.Sprintf(`{"id": %d, "status": %q, "conclusion": %s, "html_url": %q, "display_title": %q, "created_at": %q}`,
		id, status, c, runURL(id), title, created.UTC().Format(time.RFC3339))
}

func listJSON(runs ...string) string {
	return fmt.Sprintf(`{"total_count": %d, "workflow_runs": [%s]}`, len(runs), strings.Join(runs, ","))
}

var widgets42 = Request{Repo: "acme/widgets", PR: 42, Action: ActionRun}

// doerFunc answers every request the same way, for waits whose number of polls
// is not the point.
type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func answer(status int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestTheRelayRunIsTitledByItsRequest(t *testing.T) {
	if got := runName(widgets42); got != "run acme/widgets#42" {
		t.Errorf("the run is titled %q, want %q", got, "run acme/widgets#42")
	}
}

// An earlier dispatch of the same request has the same title. Taking it would
// report how a previous review ended as this one's outcome.
func TestAwaitNeverTakesARunFromBeforeTheDispatch(t *testing.T) {
	since := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	net := &scripted{t: t, exchanges: []exchange{
		{method: http.MethodGet, path: listPath, status: 200, body: listJSON(
			runJSON(503, "queued", "", "run acme/widgets#42", since.Add(3*time.Second)),
			runJSON(502, "in_progress", "", "approve acme/widgets#42", since.Add(2*time.Second)),
			runJSON(501, "completed", "success", "run acme/widgets#42", since.Add(-time.Hour)),
		)},
		{method: http.MethodGet, path: runPath(503), status: 200,
			body: runJSON(503, "in_progress", "", "run acme/widgets#42", since.Add(3*time.Second))},
		{method: http.MethodGet, path: runPath(503), status: 200,
			body: runJSON(503, "completed", "failure", "run acme/widgets#42", since.Add(3*time.Second))},
	}}
	got, err := await(context.Background(), net, target, Dispatched{Request: widgets42, Since: since}, time.Minute, fastPoll)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if got.Conclusion != "failure" || got.URL != runURL(503) {
		t.Errorf("got %+v, want run 503's failure", got)
	}
	if q := net.seen[0].URL.Query().Get("per_page"); q != "100" {
		t.Errorf("the list reads a page of %q runs, want 100", q)
	}
}

// GitHub's created_at is whole seconds, and a dispatch is rarely sent on one.
func TestAwaitTakesARunStartedInTheSecondOfTheDispatch(t *testing.T) {
	since := time.Date(2026, 9, 28, 12, 0, 0, 700_000_000, time.UTC)
	net := &scripted{t: t, exchanges: []exchange{
		{method: http.MethodGet, path: listPath, status: 200, body: listJSON(
			runJSON(510, "completed", "success", "run acme/widgets#42", since.Truncate(time.Second)),
		)},
	}}
	got, err := await(context.Background(), net, target, Dispatched{Request: widgets42, Since: since}, time.Minute, fastPoll)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if got.Conclusion != "success" || got.URL != runURL(510) {
		t.Errorf("got %+v, want run 510's success", got)
	}
}

func TestAwaitKeepsLookingUntilTheRunAppears(t *testing.T) {
	since := time.Now()
	net := &scripted{t: t, exchanges: []exchange{
		{method: http.MethodGet, path: listPath, status: 200, body: listJSON()},
		{method: http.MethodGet, path: listPath, status: 200, body: listJSON(
			runJSON(520, "queued", "", "run acme/widgets#42", since.Add(time.Second)),
		)},
		{method: http.MethodGet, path: runPath(520), status: 200,
			body: runJSON(520, "completed", "cancelled", "run acme/widgets#42", since.Add(time.Second))},
	}}
	got, err := await(context.Background(), net, target, Dispatched{Request: widgets42, Since: since}, time.Minute, fastPoll)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if got.Conclusion != "cancelled" {
		t.Errorf("got %+v, want the run's cancellation", got)
	}
}

// A run GitHub named on dispatch is read directly; its title is never
// consulted, so a relay titled any other way is still followed.
func TestAwaitFollowsTheRunTheDispatchNamed(t *testing.T) {
	net := &scripted{t: t, exchanges: []exchange{
		{method: http.MethodGet, path: runPath(530), status: 200,
			body: runJSON(530, "queued", "", "Relay", time.Now())},
		{method: http.MethodGet, path: runPath(530), status: 200,
			body: runJSON(530, "completed", "success", "Relay", time.Now())},
	}}
	d := Dispatched{Request: widgets42, Since: time.Now(), RunID: 530, URL: runURL(530)}
	got, err := await(context.Background(), net, target, d, time.Minute, fastPoll)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if got.Conclusion != "success" || got.URL != runURL(530) {
		t.Errorf("got %+v, want run 530's success", got)
	}
	if auth := net.seen[0].Header.Get("Authorization"); auth != "Bearer ghp_caller" {
		t.Errorf("the poll authenticated as %q", auth)
	}
}

// The relay's run goes on whether or not one read of it fails, so a failure
// GitHub might not repeat is read past.
func TestAwaitReadsPastAServerFailure(t *testing.T) {
	net := &scripted{t: t, exchanges: []exchange{
		{method: http.MethodGet, path: runPath(540), status: http.StatusBadGateway, body: `{"message": "Server Error"}`},
		{method: http.MethodGet, path: runPath(540), status: 200,
			body: runJSON(540, "completed", "success", "run acme/widgets#42", time.Now())},
	}}
	got, err := await(context.Background(), net, target, Dispatched{Request: widgets42, RunID: 540}, time.Minute, fastPoll)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if got.Conclusion != "success" {
		t.Errorf("got %+v, want the run's success", got)
	}
}

func TestAwaitStopsAtARefusal(t *testing.T) {
	net := &scripted{t: t, exchanges: []exchange{
		{method: http.MethodGet, path: runPath(550), status: http.StatusNotFound, body: `{"message": "Not Found"}`},
	}}
	_, err := await(context.Background(), net, target, Dispatched{Request: widgets42, RunID: 550}, time.Minute, fastPoll)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("a 404 came back as %v, want the refusal", err)
	}
	net.done()
}

// A rate limit clears on its own, so it is read past rather than ended at:
// the relay's own run keeps going regardless, and stopping here would report
// a failure for a run that may still succeed.
func TestAwaitReadsPastARateLimit(t *testing.T) {
	for name, blocked := range map[string]exchange{
		"secondary, Retry-After": {
			method: http.MethodGet, path: runPath(560), status: http.StatusForbidden,
			body:   `{"message": "secondary rate limit"}`,
			header: http.Header{"Retry-After": []string{"1"}},
		},
		"primary, 429": {
			method: http.MethodGet, path: runPath(560), status: http.StatusTooManyRequests,
			body: `{"message": "rate limit exceeded"}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			net := &scripted{t: t, exchanges: []exchange{
				blocked,
				{method: http.MethodGet, path: runPath(560), status: 200,
					body: runJSON(560, "completed", "success", "run acme/widgets#42", time.Now())},
			}}
			got, err := await(context.Background(), net, target, Dispatched{Request: widgets42, RunID: 560}, time.Minute, fastPoll)
			if err != nil {
				t.Fatal(err)
			}
			net.done()
			if got.Conclusion != "success" {
				t.Errorf("got %+v, want the run's success", got)
			}
		})
	}
}

// A 403 with no rate-limit header is an ordinary refusal — a token the relay
// repository refuses outright, say — and reading it as a rate limit would
// turn a refusal that will never change into one that waits out its timeout.
func TestAwaitStopsAtAPlainForbidden(t *testing.T) {
	net := &scripted{t: t, exchanges: []exchange{
		{method: http.MethodGet, path: runPath(570), status: http.StatusForbidden, body: `{"message": "Forbidden"}`},
	}}
	_, err := await(context.Background(), net, target, Dispatched{Request: widgets42, RunID: 570}, time.Minute, fastPoll)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("a plain 403 came back as %v, want the refusal", err)
	}
	net.done()
}

// retryAfter is what decides how long a rate limit is waited out, so its own
// duration is worth pinning down directly, without engaging await's loop or
// sleeping out a real rate limit to do it.
func TestRetryAfterReadsTheResetOffARateLimit(t *testing.T) {
	reset := time.Now().Add(90 * time.Second)
	limited := &Error{StatusCode: http.StatusTooManyRequests, RetryAfter: reset}
	if got := retryAfter(limited); got <= 89*time.Second || got > 90*time.Second {
		t.Errorf("retryAfter(%+v) = %s, want close to 90s", limited, got)
	}

	notLimited := &Error{StatusCode: http.StatusNotFound}
	if got := retryAfter(notLimited); got != 0 {
		t.Errorf("retryAfter(%+v) = %s, want 0", notLimited, got)
	}

	if got := retryAfter(errors.New("a transport failure, not GitHub's own refusal")); got != 0 {
		t.Errorf("retryAfter of a non-API error = %s, want 0", got)
	}
}

func TestAwaitGivesUpWhenNoRunAppears(t *testing.T) {
	never := doerFunc(func(*http.Request) (*http.Response, error) { return answer(200, listJSON()) })
	start := time.Now()
	got, err := await(context.Background(), never, target, Dispatched{Request: widgets42, Since: start}, 50*time.Millisecond, fastPoll)
	if err == nil {
		t.Fatalf("a run that never appeared completed as %+v", got)
	}
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), `no relay run "run acme/widgets#42" appeared`) {
		t.Errorf("the timeout reads %q", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("a 50ms timeout took %v", elapsed)
	}
}

func TestAwaitGivesUpWhenTheRunNeverCompletes(t *testing.T) {
	stuck := doerFunc(func(*http.Request) (*http.Response, error) {
		return answer(200, runJSON(560, "in_progress", "", "run acme/widgets#42", time.Now()))
	})
	got, err := await(context.Background(), stuck, target, Dispatched{Request: widgets42, RunID: 560}, 50*time.Millisecond, fastPoll)
	if err == nil {
		t.Fatalf("a run that never completed completed as %+v", got)
	}
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), runURL(560)+" did not complete") {
		t.Errorf("the timeout reads %q", err)
	}
}

// The last failed read is what a person needs to see when the wait runs out,
// because it is usually why.
func TestAwaitGivesUpWithTheLastFailure(t *testing.T) {
	failing := doerFunc(func(*http.Request) (*http.Response, error) {
		return answer(http.StatusServiceUnavailable, `{"message": "Service Unavailable"}`)
	})
	_, err := await(context.Background(), failing, target, Dispatched{Request: widgets42, RunID: 570}, 50*time.Millisecond, fastPoll)
	if err == nil || !strings.Contains(err.Error(), "Service Unavailable") {
		t.Errorf("the timeout reads %v, without the failure behind it", err)
	}
}

// A person pressing Ctrl-C is not made to sit out the poll interval.
func TestAwaitStopsWhenCancelled(t *testing.T) {
	stuck := doerFunc(func(*http.Request) (*http.Response, error) {
		return answer(200, runJSON(580, "in_progress", "", "run acme/widgets#42", time.Now()))
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := await(ctx, stuck, target, Dispatched{Request: widgets42, RunID: 580}, time.Hour, time.Hour)
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("a cancelled wait returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled wait was still polling two seconds later")
	}
}
