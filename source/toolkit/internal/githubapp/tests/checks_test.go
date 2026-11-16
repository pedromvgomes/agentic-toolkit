package tests

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

const (
	headSHA           = "0123456789abcdef0123456789abcdef01234567"
	checkRunsPath     = "/repos/acme/widgets/commits/" + headSHA + "/check-runs"
	statusPath        = "/repos/acme/widgets/commits/" + headSHA + "/status"
	issueCommentsPath = "/repos/acme/widgets/issues/12/comments"
	checksPageLength  = 25
	commentPageLength = 25
)

// checkRunsJSON renders a page of count check runs numbered from first, under
// a total of total.
func checkRunsJSON(total, first, count int) string {
	runs := make([]string, count)
	for i := range runs {
		runs[i] = fmt.Sprintf(`{"name":"job %d","status":"completed","conclusion":"success",`+
			`"html_url":"https://github.test/run/%d","details_url":"https://ci.test/%d",`+
			`"app":{"slug":"github-actions"},"output":{"summary":"ok"}}`, first+i, first+i, first+i)
	}
	return fmt.Sprintf(`{"total_count":%d,"check_runs":[%s]}`, total, strings.Join(runs, ","))
}

// commentsJSON renders a page of count comments with ids from first.
func commentsJSON(first, count int) string {
	items := make([]string, count)
	for i := range items {
		items[i] = fmt.Sprintf(`{"id":%d,"user":{"login":"octocat"},"body":"comment %d",`+
			`"created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-01T11:00:00Z"}`, first+i, first+i)
	}
	return "[" + strings.Join(items, ",") + "]"
}

func TestCheckRunsAreReadWithTheirStateLinksAndApp(t *testing.T) {
	c, net := readClient(t, get(checkRunsPath, 200, `{"total_count":2,"check_runs":[`+
		`{"name":"build","status":"completed","conclusion":"failure","html_url":"https://github.test/r/1",`+
		`"details_url":"https://ci.test/1","app":{"slug":"github-actions"}},`+
		`{"name":"lint","status":"in_progress","conclusion":null,"html_url":"https://github.test/r/2",`+
		`"details_url":"https://ci.test/2","app":null}]}`))
	runs, err := c.ReadCheckRuns(context.Background(), headSHA)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	want := []githubapp.CheckRun{
		{Name: "build", Status: "completed", Conclusion: "failure", HTMLURL: "https://github.test/r/1",
			DetailsURL: "https://ci.test/1", App: "github-actions"},
		{Name: "lint", Status: "in_progress", HTMLURL: "https://github.test/r/2", DetailsURL: "https://ci.test/2"},
	}
	if fmt.Sprint(runs) != fmt.Sprint(want) {
		t.Errorf("read %+v, want %+v", runs, want)
	}
	if got := net.seen[0].URL.Query().Get("filter"); got != "latest" {
		t.Errorf("check runs were read with filter %q, want latest", got)
	}
}

func TestCheckRunsAreReadAcrossPagesUntilTheReportedCount(t *testing.T) {
	c, net := readClient(t,
		get(checkRunsPath, 200, checkRunsJSON(checksPageLength+3, 1, checksPageLength)),
		get(checkRunsPath, 200, checkRunsJSON(checksPageLength+3, checksPageLength+1, 3)))
	runs, err := c.ReadCheckRuns(context.Background(), headSHA)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if len(runs) != checksPageLength+3 || runs[checksPageLength].Name != fmt.Sprintf("job %d", checksPageLength+1) {
		t.Errorf("read %d runs across two pages, want %d", len(runs), checksPageLength+3)
	}
	for i, page := range []string{"1", "2"} {
		if got := net.seen[i].URL.Query().Get("page"); got != page {
			t.Errorf("call %d read page %q, want %s", i+1, got, page)
		}
	}
}

func TestACommitWithNoCheckRunsReadsAsAnEmptyList(t *testing.T) {
	c, net := readClient(t, get(checkRunsPath, 200, `{"total_count":0,"check_runs":[]}`))
	runs, err := c.ReadCheckRuns(context.Background(), headSHA)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if runs == nil || len(runs) != 0 {
		t.Errorf("read %#v, want an empty list", runs)
	}
}

// A list missing its one failing run reads as a green commit, so a read that
// does not add up to the count GitHub reports is refused.
func TestACheckRunReadThatDoesNotMatchItsCountIsRefused(t *testing.T) {
	cases := map[string][]exchange{
		"pages end early": {
			get(checkRunsPath, 200, checkRunsJSON(checksPageLength+3, 1, checksPageLength)),
			get(checkRunsPath, 200, checkRunsJSON(checksPageLength+3, checksPageLength+1, 0)),
		},
		"more runs than reported": {
			get(checkRunsPath, 200, checkRunsJSON(2, 1, 3)),
		},
		"count changes between pages": {
			get(checkRunsPath, 200, checkRunsJSON(checksPageLength+3, 1, checksPageLength)),
			get(checkRunsPath, 200, checkRunsJSON(checksPageLength+4, checksPageLength+1, 3)),
		},
		"no count": {
			get(checkRunsPath, 200, `{"check_runs":[]}`),
		},
		"a run with no status": {
			get(checkRunsPath, 200, `{"total_count":1,"check_runs":[{"name":"build"}]}`),
		},
	}
	for name, exchanges := range cases {
		t.Run(name, func(t *testing.T) {
			c, net := readClient(t, exchanges...)
			runs, err := c.ReadCheckRuns(context.Background(), headSHA)
			if err == nil {
				t.Fatalf("read %d runs, want a refusal", len(runs))
			}
			net.done()
		})
	}
}

func TestCommitStatusesAreReadWithTheCombinedState(t *testing.T) {
	c, net := readClient(t, get(statusPath, 200, `{"state":"pending","total_count":2,"statuses":[`+
		`{"context":"lydite/referral","state":"pending","description":"waiting","target_url":"https://lydite.test/1"},`+
		`{"context":"ci/legacy","state":"success","description":null,"target_url":null}]}`))
	combined, err := c.ReadCommitStatuses(context.Background(), headSHA)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	want := githubapp.CombinedStatus{State: "pending", Statuses: []githubapp.CommitStatus{
		{Context: "lydite/referral", State: "pending", Description: "waiting", TargetURL: "https://lydite.test/1"},
		{Context: "ci/legacy", State: "success"},
	}}
	if fmt.Sprint(combined) != fmt.Sprint(want) {
		t.Errorf("read %+v, want %+v", combined, want)
	}
}

func TestCommitStatusesAreReadAcrossPagesUntilTheReportedCount(t *testing.T) {
	statuses := func(first, count int) string {
		items := make([]string, count)
		for i := range items {
			items[i] = fmt.Sprintf(`{"context":"ctx/%d","state":"success"}`, first+i)
		}
		return strings.Join(items, ",")
	}
	c, net := readClient(t,
		get(statusPath, 200, `{"state":"success","total_count":101,"statuses":[`+statuses(1, 100)+`]}`),
		get(statusPath, 200, `{"state":"success","total_count":101,"statuses":[`+statuses(101, 1)+`]}`))
	combined, err := c.ReadCommitStatuses(context.Background(), headSHA)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if len(combined.Statuses) != 101 || combined.Statuses[100].Context != "ctx/101" {
		t.Errorf("read %d statuses across two pages, want 101", len(combined.Statuses))
	}
	if got := net.seen[1].URL.Query().Get("page"); got != "2" {
		t.Errorf("the second call read page %q, want 2", got)
	}
}

func TestACommitStatusReadThatDoesNotMatchItsCountIsRefused(t *testing.T) {
	cases := map[string][]exchange{
		"pages end early": {
			get(statusPath, 200, `{"state":"success","total_count":2,"statuses":[{"context":"a","state":"success"}]}`),
			get(statusPath, 200, `{"state":"success","total_count":2,"statuses":[]}`),
		},
		"more statuses than reported": {
			get(statusPath, 200, `{"state":"success","total_count":0,"statuses":[{"context":"a","state":"success"}]}`),
		},
		"no combined state": {
			get(statusPath, 200, `{"total_count":0,"statuses":[]}`),
		},
		"combined state changes between pages": {
			get(statusPath, 200, `{"state":"success","total_count":2,"statuses":[{"context":"a","state":"success"}]}`),
			get(statusPath, 200, `{"state":"failure","total_count":2,"statuses":[{"context":"b","state":"failure"}]}`),
		},
	}
	for name, exchanges := range cases {
		t.Run(name, func(t *testing.T) {
			c, net := readClient(t, exchanges...)
			combined, err := c.ReadCommitStatuses(context.Background(), headSHA)
			if err == nil {
				t.Fatalf("read %+v, want a refusal", combined)
			}
			net.done()
		})
	}
}

func TestIssueCommentsAreReadWithTheirAuthorBodyAndTimes(t *testing.T) {
	c, net := readClient(t, get(issueCommentsPath, 200, `[`+
		`{"id":7,"user":{"login":"lydite[bot]"},"body":"<!-- lydite:results -->\nall good",`+
		`"created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-02T10:00:00Z"},`+
		`{"id":8,"user":null,"body":"gone","created_at":"2026-11-03T10:00:00Z","updated_at":"2026-11-03T10:00:00Z"}]`))
	comments, err := c.ReadIssueComments(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if len(comments) != 2 {
		t.Fatalf("read %d comments, want 2", len(comments))
	}
	first := comments[0]
	if first.ID != 7 || first.Author != "lydite[bot]" || first.Body != "<!-- lydite:results -->\nall good" ||
		!first.CreatedAt.Equal(time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)) ||
		!first.UpdatedAt.Equal(time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("read %+v", first)
	}
	if comments[1].Author != "" {
		t.Errorf("a comment by a deleted account names %q", comments[1].Author)
	}
}

func TestIssueCommentsAreReadAcrossPages(t *testing.T) {
	c, net := readClient(t,
		get(issueCommentsPath, 200, commentsJSON(1, commentPageLength)),
		get(issueCommentsPath, 200, commentsJSON(commentPageLength+1, 2)))
	comments, err := c.ReadIssueComments(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if len(comments) != commentPageLength+2 || comments[commentPageLength].ID != commentPageLength+1 {
		t.Errorf("read %d comments across two pages, want %d", len(comments), commentPageLength+2)
	}
	if got := net.seen[1].URL.Query().Get("page"); got != "2" {
		t.Errorf("the second call read page %q, want 2", got)
	}
}

// A comment read twice means the pages shifted under the read, and a list read
// across a shift can be missing the comment the caller is looking for.
func TestAnIssueCommentReadThatRepeatsACommentIsRefused(t *testing.T) {
	c, net := readClient(t,
		get(issueCommentsPath, 200, commentsJSON(1, commentPageLength)),
		get(issueCommentsPath, 200, commentsJSON(commentPageLength, 2)))
	if comments, err := c.ReadIssueComments(context.Background(), 12); err == nil {
		t.Fatalf("read %d comments, want a refusal", len(comments))
	}
	net.done()
}

func TestAnIssueCommentReadThatNeverEndsIsRefused(t *testing.T) {
	var exchanges []exchange
	for page := range 40 {
		exchanges = append(exchanges, get(issueCommentsPath, 200, commentsJSON(page*commentPageLength+1, commentPageLength)))
	}
	c, net := readClient(t, exchanges...)
	if comments, err := c.ReadIssueComments(context.Background(), 12); err == nil {
		t.Fatalf("read %d comments, want a refusal", len(comments))
	}
	net.done()
}

// The commits endpoints resolve an abbreviation or a branch name when the
// request lands, so only a full hash names the commit the caller read.
func TestACIReadRefusesAnythingButAFullCommitSHA(t *testing.T) {
	for _, sha := range []string{"", "main", "0123456", headSHA + "0", strings.Repeat("g", 40), "../../" + headSHA[6:]} {
		c, net := readClient(t)
		if _, err := c.ReadCheckRuns(context.Background(), sha); err == nil {
			t.Errorf("check runs were read for %q", sha)
		}
		if _, err := c.ReadCommitStatuses(context.Background(), sha); err == nil {
			t.Errorf("statuses were read for %q", sha)
		}
		net.done()
	}
	for _, number := range []int{0, -1} {
		c, net := readClient(t)
		if _, err := c.ReadIssueComments(context.Background(), number); err == nil {
			t.Errorf("comments were read for issue %d", number)
		}
		net.done()
	}
}

func TestACIReadThatGitHubRefusesIsClassified(t *testing.T) {
	c, _ := readClient(t, get(checkRunsPath, 404, `{"message":"Not Found"}`))
	if _, err := c.ReadCheckRuns(context.Background(), headSHA); !errors.Is(err, githubapp.ErrNotFound) {
		t.Errorf("a 404 on check runs read as %v", err)
	}
	c, _ = readClient(t, get(statusPath, 403, `{"message":"Resource not accessible"}`))
	if _, err := c.ReadCommitStatuses(context.Background(), headSHA); !errors.Is(err, githubapp.ErrForbidden) {
		t.Errorf("a 403 on statuses read as %v", err)
	}
	c, _ = readClient(t, exchange{method: http.MethodGet, path: issueCommentsPath, status: 403,
		body:   `{"message":"API rate limit exceeded"}`,
		header: http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"2000000000"}}})
	_, err := c.ReadIssueComments(context.Background(), 12)
	var apiErr *githubapp.Error
	if errors.Is(err, githubapp.ErrForbidden) || !errors.As(err, &apiErr) || !apiErr.RateLimited() {
		t.Errorf("a rate-limited comment read read as %v", err)
	}
}

// Every CI read is a GET with no body under the caller's token: a token client
// that posted would be invisible to the reads that decide what was reviewed.
func TestEveryCIReadIsAGetUnderTheCallersToken(t *testing.T) {
	c, net := readClient(t,
		get(checkRunsPath, 200, `{"total_count":0,"check_runs":[]}`),
		get(statusPath, 200, `{"state":"pending","total_count":0,"statuses":[]}`),
		get(issueCommentsPath, 200, `[]`))
	ctx := context.Background()
	if _, err := c.ReadCheckRuns(ctx, headSHA); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadCommitStatuses(ctx, headSHA); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadIssueComments(ctx, 12); err != nil {
		t.Fatal(err)
	}
	net.done()
	for i, req := range net.seen {
		if req.Method != http.MethodGet {
			t.Errorf("call %d is %s %s", i+1, req.Method, req.URL.Path)
		}
		if net.bodies[i] != "" {
			t.Errorf("call %d sent a body: %s", i+1, net.bodies[i])
		}
		if req.Header.Get("Authorization") != "Bearer ghp_scripted" {
			t.Errorf("call %d did not carry the caller's token", i+1)
		}
	}
}

// A hash GitHub prints in upper case names the same commit, so every
// hexadecimal digit is accepted in either case, the range ends included.
func TestACIReadAcceptsAFullCommitSHAInUpperCase(t *testing.T) {
	sha := "ABCDEF0123456789ABCDEF0123456789ABCDEF01"
	c, net := readClient(t,
		get("/repos/acme/widgets/commits/"+sha+"/check-runs", 200, `{"total_count":0,"check_runs":[]}`),
		get("/repos/acme/widgets/commits/"+sha+"/status", 200, `{"state":"pending","total_count":0,"statuses":[]}`))
	if _, err := c.ReadCheckRuns(context.Background(), sha); err != nil {
		t.Errorf("check runs for %q: %v", sha, err)
	}
	if _, err := c.ReadCommitStatuses(context.Background(), sha); err != nil {
		t.Errorf("statuses for %q: %v", sha, err)
	}
	net.done()
}

func TestTheCommentsOnIssueOneAreRead(t *testing.T) {
	c, net := readClient(t, get("/repos/acme/widgets/issues/1/comments", 200, commentsJSON(1, 1)))
	comments, err := c.ReadIssueComments(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if len(comments) != 1 {
		t.Errorf("read %d comments, want 1", len(comments))
	}
}

// The page limit refuses a read still going after its last page, never one
// that ends on it.
func TestACIReadThatEndsOnItsLastAllowedPageIsReturned(t *testing.T) {
	const pages = 40
	var runPages []exchange
	for page := range pages {
		runPages = append(runPages, get(checkRunsPath, 200,
			checkRunsJSON(pages*checksPageLength, page*checksPageLength+1, checksPageLength)))
	}
	c, net := readClient(t, runPages...)
	runs, err := c.ReadCheckRuns(context.Background(), headSHA)
	if err != nil {
		t.Fatalf("check runs ending on page %d: %v", pages, err)
	}
	net.done()
	if len(runs) != pages*checksPageLength {
		t.Errorf("read %d check runs, want %d", len(runs), pages*checksPageLength)
	}

	const statusPageLength = 100
	var statusPages []exchange
	for page := range pages {
		items := make([]string, statusPageLength)
		for i := range items {
			items[i] = fmt.Sprintf(`{"context":"ctx/%d","state":"success"}`, page*statusPageLength+i+1)
		}
		statusPages = append(statusPages, get(statusPath, 200, fmt.Sprintf(`{"state":"success","total_count":%d,"statuses":[%s]}`,
			pages*statusPageLength, strings.Join(items, ","))))
	}
	c, net = readClient(t, statusPages...)
	combined, err := c.ReadCommitStatuses(context.Background(), headSHA)
	if err != nil {
		t.Fatalf("statuses ending on page %d: %v", pages, err)
	}
	net.done()
	if len(combined.Statuses) != pages*statusPageLength {
		t.Errorf("read %d statuses, want %d", len(combined.Statuses), pages*statusPageLength)
	}
}
