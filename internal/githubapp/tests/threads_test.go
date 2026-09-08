package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

const graphqlPath = "/graphql"

// far is an expiry no test reaches, so the token is minted once.
func far() time.Time { return time.Now().Add(time.Hour) }

// query answers one GraphQL call with a 200 carrying data.
func query(body string) exchange {
	return exchange{method: http.MethodPost, path: graphqlPath, status: 200, body: body}
}

// threadsPage renders one page of the reviewThreads connection.
func threadsPage(nodes string, hasNext bool, cursor string) string {
	return `{"data":{"repository":{"pullRequest":{"reviewThreads":{` +
		`"pageInfo":{"hasNextPage":` + boolText(hasNext) + `,"endCursor":"` + cursor + `"},` +
		`"nodes":[` + nodes + `]}}}}}`
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// threadNode renders one thread as GraphQL reports it.
func threadNode(path string, resolved, outdated, byViewer bool, body string) string {
	encoded, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return `{"path":"` + path + `","isResolved":` + boolText(resolved) +
		`,"isOutdated":` + boolText(outdated) +
		`,"comments":{"nodes":[{"body":` + string(encoded) + `,"viewerDidAuthor":` + boolText(byViewer) + `}]}}`
}

// Resolution and staleness are the two states REST cannot report — the
// comments endpoint answers `position: null` for a comment whose code moved
// and says nothing at all about resolution — and they oblige opposite things.
func TestAThreadCarriesItsResolutionAndItsStaleness(t *testing.T) {
	c, net := client(t, append(auth(far()), query(threadsPage(
		threadNode("a.go", false, false, true, "open by the app")+","+
			threadNode("b.go", true, false, false, "resolved by a person")+","+
			threadNode("c.go", false, true, true, "outdated"),
		false, "")))...)

	threads, err := c.ReadReviewThreads(context.Background(), 7)
	if err != nil {
		t.Fatalf("read the threads: %v", err)
	}
	net.done()

	want := []githubapp.ReviewThread{
		{Path: "a.go", Body: "open by the app", ByViewer: true},
		{Path: "b.go", Resolved: true, Body: "resolved by a person"},
		{Path: "c.go", Outdated: true, Body: "outdated", ByViewer: true},
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

// A GraphQL refusal arrives inside a 200. Read by status alone it is a
// successful query that found nothing — which for the read that decides what a
// review withholds is the difference between "already on the pull request" and
// "post it again".
func TestAGraphQLRefusalInsideA200IsAFailure(t *testing.T) {
	c, net := client(t, append(auth(far()), query(
		`{"data":null,"errors":[{"type":"FORBIDDEN","message":"Resource not accessible by integration"}]}`))...)

	threads, err := c.ReadReviewThreads(context.Background(), 7)
	net.done()
	if err == nil {
		t.Fatalf("a 200 carrying errors was read as an answer: %+v", threads)
	}
	var gqlErr *githubapp.GraphQLError
	if !errors.As(err, &gqlErr) {
		t.Fatalf("the refusal is not a GraphQLError: %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "FORBIDDEN") || !strings.Contains(err.Error(), "review threads") {
		t.Errorf("the refusal does not say what was refused: %v", err)
	}
}

// Partial data beside an errors array is still a refusal. Taking the data
// would return a thread list that is short by however much the error cost,
// and a short thread list is a finding posted a second time.
func TestPartialDataBesideAnErrorIsStillARefusal(t *testing.T) {
	c, net := client(t, append(auth(far()), query(
		`{"data":{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[]}}}},`+
			`"errors":[{"message":"Something went wrong"}]}`))...)

	if _, err := c.ReadReviewThreads(context.Background(), 7); err == nil {
		t.Error("an errors array beside data was read as an answer")
	}
	net.done()
}

// A 200 whose data is null carries no answer, and an empty thread list read
// out of it would report a pull request as carrying nothing.
func TestAnAnswerWithNoDataIsARefusal(t *testing.T) {
	c, net := client(t, append(auth(far()), query(`{"data":null}`))...)

	_, err := c.ReadReviewThreads(context.Background(), 7)
	net.done()
	// A GraphQLError specifically: `null` decodes into the answer struct
	// without complaint and leaves every field zero, so a reader that did not
	// check would fall through to "GitHub reported no pull request 7" and
	// blame the pull request for a malformed answer.
	var gqlErr *githubapp.GraphQLError
	if !errors.As(err, &gqlErr) {
		t.Fatalf("an answer with no data was not read as a refused query: %T %v", err, err)
	}
}

// GraphQL is served over the same transport as everything else, so an HTTP
// refusal reaches the caller as the typed refusal every other call makes.
func TestATransportRefusalMidQueryIsReported(t *testing.T) {
	c, net := client(t, append(auth(far()),
		exchange{method: http.MethodPost, path: graphqlPath, status: 502, body: `{"message": "Bad gateway"}`})...)

	_, err := c.ReadReviewThreads(context.Background(), 7)
	net.done()
	var apiErr *githubapp.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("a 502 is not a githubapp.Error: %T %v", err, err)
	}
	if apiErr.StatusCode != 502 {
		t.Errorf("the refusal reports status %d", apiErr.StatusCode)
	}
}

// A pull request that has been argued over carries more threads than one page
// holds, and a list that silently stopped at the first page is a finding
// posted again.
func TestEveryPageOfThreadsIsRead(t *testing.T) {
	c, net := client(t, append(auth(far()),
		query(threadsPage(threadNode("a.go", false, false, true, "first"), true, "CURSOR1")),
		query(threadsPage(threadNode("b.go", false, false, true, "second"), false, "")))...)

	threads, err := c.ReadReviewThreads(context.Background(), 7)
	if err != nil {
		t.Fatalf("read the threads: %v", err)
	}
	net.done()
	if len(threads) != 2 || threads[0].Body != "first" || threads[1].Body != "second" {
		t.Fatalf("the pages were not joined: %+v", threads)
	}
	// The second call has to carry the first page's cursor, or two identical
	// requests would return the same page twice and still look like paging.
	if !strings.Contains(net.bodies[len(net.bodies)-1], "CURSOR1") {
		t.Errorf("the second page was asked for without the first page's cursor: %s", net.bodies[len(net.bodies)-1])
	}
}

// A server that keeps answering "there is more" is a loop, and a loop against
// a rate-limited API is worse than a refusal.
func TestAThreadListThatNeverEndsIsRefusedRatherThanLoopedOn(t *testing.T) {
	var exchanges []exchange
	exchanges = append(exchanges, auth(far())...)
	// One more page than the bound allows, so reaching the bound is what stops
	// it rather than running out of script.
	for i := 0; i <= 40; i++ {
		exchanges = append(exchanges, query(threadsPage(threadNode("a.go", false, false, true, "x"), true, "SAME")))
	}
	c, net := client(t, exchanges...)

	_, err := c.ReadReviewThreads(context.Background(), 7)
	if err == nil {
		t.Fatal("an endless thread list was read as an answer")
	}
	if !strings.Contains(err.Error(), "did not end") {
		t.Errorf("the refusal does not say the list never ended: %v", err)
	}
	if len(net.seen) > 42 {
		t.Errorf("the read made %d calls, so the bound did not stop it", len(net.seen))
	}
}

// A review posted by somebody else says nothing about whether this App has
// reviewed the head, and counting one would make every re-run a no-op on a
// pull request a person has reviewed.
func TestOnlyTheAppsOwnReviewsCountAsHavingReviewedAHead(t *testing.T) {
	c, net := client(t, append(auth(far()), query(
		`{"data":{"repository":{"pullRequest":{"reviews":{"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[`+
			`{"commit":{"oid":"aaa"},"viewerDidAuthor":true},`+
			`{"commit":{"oid":"bbb"},"viewerDidAuthor":false},`+
			`{"commit":null,"viewerDidAuthor":true}]}}}}}`))...)

	reviewed, err := c.ReadReviewedCommits(context.Background(), 7)
	if err != nil {
		t.Fatalf("read the reviews: %v", err)
	}
	net.done()
	if !reviewed["aaa"] {
		t.Error("the App's own review of aaa was not counted")
	}
	if reviewed["bbb"] {
		t.Error("somebody else's review of bbb was counted as the App's")
	}
	if len(reviewed) != 1 {
		t.Errorf("read %d reviewed commits: %v", len(reviewed), reviewed)
	}
}

// A pull request number below one addresses nothing, and interpolating it
// would ask GitHub about a resource nobody named.
func TestReadingThreadsRefusesANumberThatIsNotOne(t *testing.T) {
	c, _ := client(t)
	if _, err := c.ReadReviewThreads(context.Background(), 0); err == nil {
		t.Error("thread 0 was accepted as a pull request number")
	}
	if _, err := c.ReadReviewedCommits(context.Background(), -1); err == nil {
		t.Error("review -1 was accepted as a pull request number")
	}
}

// The page size is one number. A constant that only reached the error messages
// would let the reported page size drift from the size actually asked for.
func TestTheQueriesAskForThePageSizeTheyReport(t *testing.T) {
	c, net := client(t, append(auth(far()), query(threadsPage("", false, "")))...)
	if _, err := c.ReadReviewThreads(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	sent := net.bodies[len(net.bodies)-1]

	// The size the query asks for, read out of what was actually sent.
	asked := regexp.MustCompile(`reviewThreads\(first:(\d+)`).FindStringSubmatch(sent)
	if asked == nil {
		t.Fatalf("the query does not name a page size: %s", sent)
	}
	// And the size the refusal reports, from the same constant.
	c2, net2 := client(t, buildEndlessThreadPages()...)
	_, err := c2.ReadReviewThreads(context.Background(), 7)
	_ = net2
	if err == nil {
		t.Fatal("an endless thread list was read as an answer")
	}
	if !strings.Contains(err.Error(), "pages of "+asked[1]) {
		t.Errorf("the query asks for %s per page and the refusal reports a different size: %v", asked[1], err)
	}
}

// buildEndlessThreadPages scripts a server that never stops paging.
func buildEndlessThreadPages() []exchange {
	exchanges := auth(far())
	for i := 0; i <= 40; i++ {
		exchanges = append(exchanges, query(threadsPage(
			threadNode("a.go", false, false, true, "x"), true, "SAME")))
	}
	return exchanges
}
