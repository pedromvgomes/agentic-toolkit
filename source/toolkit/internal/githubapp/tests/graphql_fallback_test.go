package tests

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

const (
	ccrThreadsPath = "/repos/acme/widgets/pulls/7/ccr/review_threads"
	commentPath1   = "/repos/acme/widgets/pulls/comments/1"
	commentPath2   = "/repos/acme/widgets/pulls/comments/2"
)

// blockedByProxy is the refusal GitHub's own API answers a GraphQL query with
// when it is made from inside a network that blocks it, naming the
// substitute REST-shaped routes to use instead. Every test in this file
// scripts this exact refusal ahead of the fallback it expects.
func blockedByProxy() exchange {
	return exchange{method: http.MethodPost, path: graphqlPath, status: 403, body: `{"message":` +
		`"GitHub GraphQL is not available from Claude Code sessions; use the REST API ` +
		`(gh api repos/{owner}/{repo}/...). For review threads, auto-merge, and ` +
		`draft/ready-for-review use the CCR routes on api.github.com: ` +
		`GET /repos/{owner}/{repo}/pulls/{n}/ccr/review_threads"}`}
}

// A GraphQL query GitHub refused on its own merits — not because this
// network blocks GraphQL at all — is never retried over the substitute
// route: that route does not exist outside the network the refusal names,
// so retrying it there would trade one failure for a more confusing one.
func TestAnOrdinaryGraphQLRefusalIsNeverRetriedOverTheSubstituteRoute(t *testing.T) {
	c, net := client(t, append(auth(far()), query(
		`{"data":null,"errors":[{"type":"FORBIDDEN","message":"Resource not accessible by integration"}]}`))...)

	_, err := c.ReadReviewThreads(context.Background(), 7)
	net.done()
	if err == nil || !strings.Contains(err.Error(), "Resource not accessible") {
		t.Fatalf("an ordinary GraphQL refusal read as %v, want it to carry GitHub's own message", err)
	}
}

// Threads read over the substitute route carry the same resolved/outdated
// state and root body/author GraphQL would have, filled in from the ordinary
// REST endpoints the substitute route's comment ids point at.
func TestReadReviewThreadsFallsBackToTheSubstituteRouteWhenGraphQLIsBlocked(t *testing.T) {
	c, net := client(t, append(append(auth(far()), blockedByProxy()),
		exchange{method: http.MethodGet, path: ccrThreadsPath, status: 200, body: `[` +
			`{"resolved":false,"outdated":false,"path":"a.go","comment_ids":[1]},` +
			`{"resolved":true,"outdated":false,"path":"b.go","comment_ids":[2]},` +
			`{"resolved":false,"outdated":true,"path":"c.go","comment_ids":[]}` +
			`]`},
		appLogin(),
		exchange{method: http.MethodGet, path: commentPath1, status: 200,
			body: `{"body":"open by the app","user":{"login":"agtk-code-review[bot]"}}`},
		exchange{method: http.MethodGet, path: commentPath2, status: 200,
			body: `{"body":"resolved by a person","user":{"login":"someone"}}`},
	)...)

	threads, err := c.ReadReviewThreads(context.Background(), 7)
	if err != nil {
		t.Fatalf("read the threads: %v", err)
	}
	net.done()

	want := []githubapp.ReviewThread{
		{Path: "a.go", Body: "open by the app", ByViewer: true},
		{Path: "b.go", Resolved: true, Body: "resolved by a person"},
		{Path: "c.go", Outdated: true},
	}
	if len(threads) != len(want) {
		t.Fatalf("read %d threads, want %d: %+v", len(threads), len(want), threads)
	}
	for i, w := range want {
		if threads[i] != w {
			t.Errorf("thread %d is %+v, want %+v", i, threads[i], w)
		}
	}
}

// A ReadClient's viewer is whoever owns its token, never the App, so a
// thread read over the substitute route must clear ByViewer exactly as one
// read over GraphQL does — and without resolving an App login it has no use
// for.
func TestAReadClientClearsByViewerOnThreadsReadOverTheSubstituteRoute(t *testing.T) {
	threads, net := readClient(t, blockedByProxy(),
		exchange{method: http.MethodGet, path: ccrThreadsPath, status: 200, body: `[` +
			`{"resolved":false,"outdated":false,"path":"a.go","comment_ids":[1]}]`},
		exchange{method: http.MethodGet, path: commentPath1, status: 200,
			body: `{"body":"looks like the app","user":{"login":"agtk-code-review[bot]"}}`},
	)
	got, err := threads.ReadReviewThreads(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if len(got) != 1 || got[0].ByViewer {
		t.Errorf("a token client read a thread as ByViewer over the substitute route: %+v", got)
	}
	// The blocked GraphQL query, the substitute route, and the root comment's
	// body — and no fourth call resolving an App login this client has no use
	// for, since net.done() below requires every scripted call to be made and
	// no more: a fourth call here would be one this test never scripted.
	if len(net.seen) != 3 {
		t.Errorf("a token client made %d call(s), want exactly the 3 scripted", len(net.seen))
	}
}

// Every reply on a thread read over the substitute route carries its own
// body and author association, in the order the substitute route names
// them, with the root read as the thread's own body and author rather than
// as a reply.
func TestReadAnsweredThreadsFallsBackToTheSubstituteRouteWhenGraphQLIsBlocked(t *testing.T) {
	c, net := client(t, append(append(auth(far()), blockedByProxy()),
		exchange{method: http.MethodGet, path: ccrThreadsPath, status: 200, body: `[` +
			`{"resolved":false,"outdated":false,"path":"a.go","comment_ids":[1,2]}]`},
		appLogin(),
		exchange{method: http.MethodGet, path: commentPath1, status: 200,
			body: `{"body":"a finding","user":{"login":"agtk-code-review[bot]"}}`},
		exchange{method: http.MethodGet, path: commentPath2, status: 200,
			body: `{"body":"not a defect","author_association":"OWNER","user":{"login":"someone"}}`},
	)...)

	threads, err := c.ReadAnsweredThreads(context.Background(), 7)
	if err != nil {
		t.Fatalf("read the answered threads: %v", err)
	}
	net.done()

	if len(threads) != 1 {
		t.Fatalf("read %d threads, want 1: %+v", len(threads), threads)
	}
	thread := threads[0]
	if thread.Body != "a finding" || !thread.ByViewer {
		t.Errorf("the thread's own root read as %+v", thread)
	}
	if len(thread.Replies) != 1 || thread.Replies[0].Body != "not a defect" || thread.Replies[0].AuthorAssociation != "OWNER" {
		t.Errorf("the reply read as %+v", thread.Replies)
	}
}

// The App's own bot login costs one request the first time ByViewer needs
// it, and is reused rather than resolved again on a second read from the
// same client.
func TestTheAppLoginIsResolvedOnceAndReused(t *testing.T) {
	c, net := client(t, append(append(auth(far()), blockedByProxy()),
		exchange{method: http.MethodGet, path: ccrThreadsPath, status: 200, body: `[]`},
		appLogin(),
		blockedByProxy(),
		exchange{method: http.MethodGet, path: ccrThreadsPath, status: 200, body: `[]`},
	)...)

	if _, err := c.ReadReviewThreads(context.Background(), 7); err != nil {
		t.Fatalf("first read: %v", err)
	}
	if _, err := c.ReadReviewThreads(context.Background(), 7); err != nil {
		t.Fatalf("second read: %v", err)
	}
	// net.done() alone proves this: a second /app call would consume the
	// exchange scripted for the second read's blocked GraphQL query instead,
	// failing that call on a method/path mismatch rather than silently
	// passing with one call unconsumed.
	net.done()
}

// Reviews read over REST after a blocked GraphQL query carry the same body,
// commit and ByViewer a GraphQL answer would have — over REST because a
// review's body, commit and author have always had a plain REST answer,
// unlike a comment thread's resolved/outdated state.
func TestReadSubmittedReviewsFallsBackToRESTWhenGraphQLIsBlocked(t *testing.T) {
	c, net := client(t, append(append(auth(far()), blockedByProxy()),
		appLogin(),
		exchange{method: http.MethodGet, path: reviewsPath, status: 200, body: `[` +
			`{"body":"by the app","commit_id":"abc","user":{"login":"agtk-code-review[bot]"}},` +
			`{"body":"by a person","commit_id":"abc","user":{"login":"someone"}}` +
			`]`},
	)...)

	reviews, err := c.ReadSubmittedReviews(context.Background(), 7)
	if err != nil {
		t.Fatalf("read the reviews: %v", err)
	}
	net.done()

	if len(reviews) != 2 {
		t.Fatalf("read %d reviews, want 2: %+v", len(reviews), reviews)
	}
	if reviews[0].Body != "by the app" || reviews[0].CommitSHA != "abc" || !reviews[0].ByViewer {
		t.Errorf("this installation's review read back as %+v", reviews[0])
	}
	if reviews[1].ByViewer {
		t.Error("a review somebody else posted reads back as this installation's")
	}
}

// A ReadClient's viewer is whoever owns its token, never the App, so a
// review read over REST after a blocked GraphQL query must clear ByViewer
// exactly as one read over GraphQL does.
func TestAReadClientClearsByViewerOnReviewsReadOverREST(t *testing.T) {
	reviews, net := readClient(t, blockedByProxy(),
		exchange{method: http.MethodGet, path: reviewsPath, status: 200, body: `[` +
			`{"body":"looks like the app","commit_id":"abc","user":{"login":"agtk-code-review[bot]"}}]`},
	)
	got, err := reviews.ReadSubmittedReviews(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if len(got) != 1 || got[0].ByViewer {
		t.Errorf("a token client read a review as ByViewer over REST: %+v", got)
	}
	if len(net.seen) != 2 {
		t.Errorf("a token client made %d call(s), want exactly the blocked query and the REST read", len(net.seen))
	}
}
