package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

const (
	ccrThreadsPath = "/repos/acme/widgets/pulls/7/ccr/review_threads"
	prCommentsPath = "/repos/acme/widgets/pulls/7/comments"
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

// prComments renders one page of the substitute route's paginated comment
// list, the batch both proxy readers fill a thread's root and replies from.
func prComments(comments ...string) string {
	return "[" + strings.Join(comments, ",") + "]"
}

// prComment renders one comment as the substitute route's ordinary,
// paginated comments endpoint reports it.
func prComment(id int64, body, login, authorAssociation string) string {
	raw, err := json.Marshal(map[string]any{
		"id": id, "body": body, "author_association": authorAssociation,
		"user": map[string]any{"login": login},
	})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// Threads read over the substitute route carry the same resolved/outdated
// state and root body/author GraphQL would have, filled in from one
// paginated read of the pull request's own comments rather than one REST
// call per comment id.
func TestReadReviewThreadsFallsBackToTheSubstituteRouteWhenGraphQLIsBlocked(t *testing.T) {
	c, net := client(t, append(append(auth(far()), blockedByProxy()),
		exchange{method: http.MethodGet, path: ccrThreadsPath, status: 200, body: `[` +
			`{"resolved":false,"outdated":false,"path":"a.go","comment_ids":[1]},` +
			`{"resolved":true,"outdated":false,"path":"b.go","comment_ids":[2]},` +
			`{"resolved":false,"outdated":true,"path":"c.go","comment_ids":[]}` +
			`]`},
		exchange{method: http.MethodGet, path: prCommentsPath, status: 200, body: prComments(
			prComment(1, "open by the app", "agtk-code-review[bot]", ""),
			prComment(2, "resolved by a person", "someone", ""),
		)},
		appLogin(),
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
		exchange{method: http.MethodGet, path: prCommentsPath, status: 200, body: prComments(
			prComment(1, "looks like the app", "agtk-code-review[bot]", ""),
		)},
	)
	got, err := threads.ReadReviewThreads(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if len(got) != 1 || got[0].ByViewer {
		t.Errorf("a token client read a thread as ByViewer over the substitute route: %+v", got)
	}
	// The blocked GraphQL query, the substitute route, and the paginated
	// comment read — and no fourth call resolving an App login this client
	// has no use for, since net.done() below requires every scripted call to
	// be made and no more: a fourth call here would be one this test never
	// scripted.
	if len(net.seen) != 3 {
		t.Errorf("a token client made %d call(s), want exactly the 3 scripted", len(net.seen))
	}
}

// A substitute thread list missing a comment the pull request's own,
// paginated comment list reports is refused rather than answered short: the
// route it comes from answers unpaginated, unlike the GraphQL connection it
// replaces, so a caller has no other way to notice a partial answer.
func TestAThreadListMissingAKnownCommentIsRefused(t *testing.T) {
	c, net := client(t, append(append(auth(far()), blockedByProxy()),
		exchange{method: http.MethodGet, path: ccrThreadsPath, status: 200, body: `[` +
			`{"resolved":false,"outdated":false,"path":"a.go","comment_ids":[1]}]`},
		exchange{method: http.MethodGet, path: prCommentsPath, status: 200, body: prComments(
			prComment(1, "open by the app", "agtk-code-review[bot]", ""),
			prComment(2, "an open reply the thread list never named", "someone", "OWNER"),
		)},
	)...)

	_, err := c.ReadReviewThreads(context.Background(), 7)
	net.done()
	if err == nil || !strings.Contains(err.Error(), "2") {
		t.Fatalf("a thread list missing comment 2 was %v, want a refusal naming it", err)
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
		exchange{method: http.MethodGet, path: prCommentsPath, status: 200, body: prComments(
			prComment(1, "a finding", "agtk-code-review[bot]", ""),
			prComment(2, "not a defect", "someone", "OWNER"),
		)},
		appLogin(),
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

// A substitute thread list missing a comment is refused for approval's own
// read exactly as it is for a review run's: approval trusts "no thread was
// found open" only when the list it read from is known whole.
func TestReadAnsweredThreadsRefusesAThreadListMissingAKnownComment(t *testing.T) {
	c, net := client(t, append(append(auth(far()), blockedByProxy()),
		exchange{method: http.MethodGet, path: ccrThreadsPath, status: 200, body: `[]`},
		exchange{method: http.MethodGet, path: prCommentsPath, status: 200, body: prComments(
			prComment(1, "an open thread no node named", "someone", "OWNER"),
		)},
	)...)

	_, err := c.ReadAnsweredThreads(context.Background(), 7)
	net.done()
	if err == nil || !strings.Contains(err.Error(), "1") {
		t.Fatalf("a thread list missing comment 1 was %v, want a refusal naming it", err)
	}
}

// The App's own bot login costs one request the first time ByViewer needs
// it, and is reused rather than resolved again on a second read from the
// same client.
func TestTheAppLoginIsResolvedOnceAndReused(t *testing.T) {
	c, net := client(t, append(append(auth(far()), blockedByProxy()),
		exchange{method: http.MethodGet, path: ccrThreadsPath, status: 200, body: `[]`},
		exchange{method: http.MethodGet, path: prCommentsPath, status: 200, body: `[]`},
		appLogin(),
		blockedByProxy(),
		exchange{method: http.MethodGet, path: ccrThreadsPath, status: 200, body: `[]`},
		exchange{method: http.MethodGet, path: prCommentsPath, status: 200, body: `[]`},
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
