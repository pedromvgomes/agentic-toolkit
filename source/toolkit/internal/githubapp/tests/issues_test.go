package tests

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

const (
	issuePath        = "/repos/acme/widgets/issues/12"
	issuesPath       = "/repos/acme/widgets/issues"
	blockedByPath    = "/repos/acme/widgets/issues/12/dependencies/blocked_by"
	parentPath       = "/repos/acme/widgets/issues/12/parent"
	permissionPath   = "/repos/acme/widgets/collaborators/octocat/permission"
	issuesPageLength = 25
)

// issueJSON renders one issue as the REST API reports it.
func issueJSON(number int, state, reason string) string {
	return fmt.Sprintf(`{"number":%d,"title":"issue %d","state":%q,"state_reason":%q,`+
		`"user":{"login":"octocat"},"labels":[{"name":"agtk:queued"}]}`, number, number, state, reason)
}

// issuesJSON renders a page of issues numbered from first.
func issuesJSON(first, count int) string {
	items := make([]string, count)
	for i := range items {
		items[i] = issueJSON(first+i, "open", "")
	}
	return "[" + strings.Join(items, ",") + "]"
}

func get(path string, status int, body string) exchange {
	return exchange{method: http.MethodGet, path: path, status: status, body: body}
}

func TestAnIssueIsReadWithItsAuthorLabelsAndStateReason(t *testing.T) {
	c, net := readClient(t, get(issuePath, 200, `{"number":12,"title":"Do the thing","state":"closed",`+
		`"state_reason":"not_planned","user":{"login":"octocat"},`+
		`"labels":[{"name":"agtk:queued"},{"name":"bug"}]}`))
	issue, err := c.ReadIssue(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if issue.Number != 12 || issue.Title != "Do the thing" || issue.State != "closed" ||
		issue.StateReason != "not_planned" || issue.Author != "octocat" || issue.PullRequest {
		t.Errorf("read %+v", issue)
	}
	if strings.Join(issue.Labels, ",") != "agtk:queued,bug" {
		t.Errorf("read labels %v", issue.Labels)
	}
}

// REST serves pull requests through the issue endpoints, and a pull request
// read as an issue would be taken for a backlog item.
func TestAPullRequestReadAsAnIssueSaysSo(t *testing.T) {
	c, _ := readClient(t, get(issuePath, 200, `{"number":12,"state":"open","pull_request":{"url":"u"}}`))
	issue, err := c.ReadIssue(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	if !issue.PullRequest {
		t.Error("a pull request read as an issue does not report itself as one")
	}
}

// A deleted author names nobody, and must read as nobody rather than fail the
// read: who wrote the issue is the caller's decision to refuse on.
func TestAnIssueByADeletedAccountHasNoAuthor(t *testing.T) {
	c, _ := readClient(t, get(issuePath, 200, `{"number":12,"state":"open","user":null}`))
	issue, err := c.ReadIssue(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	if issue.Author != "" {
		t.Errorf("a deleted author reads as %q", issue.Author)
	}
}

// A missing issue and a refused one lead to different actions, and both
// differ from a rate limit, which is an outage rather than a judgement.
func TestNotFoundAndForbiddenAreToldApart(t *testing.T) {
	c, _ := readClient(t, get(issuePath, 404, `{"message":"Not Found"}`))
	_, err := c.ReadIssue(context.Background(), 12)
	if !errors.Is(err, githubapp.ErrNotFound) || errors.Is(err, githubapp.ErrForbidden) {
		t.Errorf("a 404 is reported as %v", err)
	}
	var apiErr *githubapp.Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 404 {
		t.Errorf("a 404 does not carry GitHub's refusal: %v", err)
	}

	c, _ = readClient(t, get(permissionPath, 403, `{"message":"Must have push access"}`))
	_, err = c.ReadPermission(context.Background(), "octocat")
	if !errors.Is(err, githubapp.ErrForbidden) || errors.Is(err, githubapp.ErrNotFound) {
		t.Errorf("a 403 is reported as %v", err)
	}

	limited := http.Header{}
	limited.Set("X-RateLimit-Remaining", "0")
	limited.Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10))
	c, _ = readClient(t, exchange{method: http.MethodGet, path: issuesPath, status: 403,
		body: `{"message":"API rate limit exceeded"}`, header: limited})
	_, err = c.ReadLabelledIssues(context.Background(), "agtk:queued")
	if errors.Is(err, githubapp.ErrForbidden) {
		t.Errorf("a rate limit is reported as a refusal: %v", err)
	}
	if !errors.As(err, &apiErr) || !apiErr.RateLimited() {
		t.Errorf("a rate limit does not report itself as one: %v", err)
	}
}

func TestAPermissionIsReadWithItsRole(t *testing.T) {
	c, net := readClient(t, get(permissionPath, 200, `{"permission":"write","role_name":"maintain"}`))
	perm, err := c.ReadPermission(context.Background(), "octocat")
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if perm.Permission != "write" || perm.RoleName != "maintain" {
		t.Errorf("read %+v", perm)
	}
}

// An App's actor login carries brackets, and has to reach the path as one
// segment.
func TestAPermissionIsReadForABotLogin(t *testing.T) {
	c, net := readClient(t, get("/repos/acme/widgets/collaborators/factory[bot]/permission", 200,
		`{"permission":"write","role_name":"write"}`))
	if _, err := c.ReadPermission(context.Background(), "factory[bot]"); err != nil {
		t.Fatal(err)
	}
	net.done()
}

// An answer with no permission in it is not "none": it is an answer nobody
// can decide on.
func TestAPermissionAnswerWithNoPermissionIsRefused(t *testing.T) {
	c, _ := readClient(t, get(permissionPath, 200, `{}`))
	if _, err := c.ReadPermission(context.Background(), "octocat"); err == nil {
		t.Fatal("an empty permission answer was accepted")
	}
}

// A list that stops at its first page is a queued item nobody dispatches or a
// blocker nobody sees.
func TestLabelledIssuesAreReadPastTheFirstPage(t *testing.T) {
	c, net := readClient(t,
		get(issuesPath, 200, issuesJSON(1, issuesPageLength)),
		get(issuesPath, 200, issuesJSON(1+issuesPageLength, 2)))
	issues, err := c.ReadLabelledIssues(context.Background(), "agtk:queued")
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if len(issues) != issuesPageLength+2 {
		t.Fatalf("read %d issues, want both pages", len(issues))
	}
	for i, req := range net.seen {
		q := req.URL.Query()
		if q.Get("page") != strconv.Itoa(i+1) || q.Get("state") != "open" || q.Get("labels") != "agtk:queued" {
			t.Errorf("call %d asked for %s", i+1, req.URL.RawQuery)
		}
	}
}

// GitHub reads a comma as a list of labels that must all be present, so a
// label with one in it would ask a different question than the caller did.
func TestALabelWithACommaNeverReachesTheNetwork(t *testing.T) {
	c, net := readClient(t)
	for _, label := range []string{"", "  ", "a,b"} {
		if _, err := c.ReadLabelledIssues(context.Background(), label); err == nil {
			t.Errorf("label %q was read", label)
		}
	}
	net.done()
}

func TestPredecessorsAreReadPastTheFirstPageWithTheirStateReasons(t *testing.T) {
	first := "[" + issueJSON(1, "closed", "completed")
	for i := 2; i <= issuesPageLength; i++ {
		first += "," + issueJSON(i, "open", "")
	}
	first += "]"
	c, net := readClient(t,
		get(blockedByPath, 200, first),
		get(blockedByPath, 200, "["+issueJSON(99, "closed", "not_planned")+"]"))
	issues, err := c.ReadPredecessors(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if len(issues) != issuesPageLength+1 {
		t.Fatalf("read %d predecessors, want both pages", len(issues))
	}
	if issues[0].StateReason != "completed" || issues[len(issues)-1].StateReason != "not_planned" {
		t.Errorf("state reasons read as %q and %q", issues[0].StateReason, issues[len(issues)-1].StateReason)
	}
	if net.seen[1].URL.Query().Get("page") != "2" {
		t.Errorf("the second call asked for %s", net.seen[1].URL.RawQuery)
	}
}

// A list that never ends is refused rather than returned short.
func TestAListThatNeverEndsIsRefused(t *testing.T) {
	var script []exchange
	for range 40 {
		script = append(script, get(blockedByPath, 200, issuesJSON(1, issuesPageLength)))
	}
	c, net := readClient(t, script...)
	if _, err := c.ReadPredecessors(context.Background(), 12); err == nil {
		t.Fatal("a list still going after the page bound was returned as complete")
	}
	net.done()
}

func TestAParentIsRead(t *testing.T) {
	c, net := readClient(t, get(parentPath, 200, issueJSON(3, "open", "")))
	parent, found, err := c.ReadParent(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if !found || parent.Number != 3 {
		t.Errorf("read parent %+v, found %v", parent, found)
	}
}

// GitHub answers 404 to an issue with no parent and to an issue that is not
// there. Only the first is "no parent".
func TestNoParentIsToldApartFromNoIssue(t *testing.T) {
	c, net := readClient(t,
		get(parentPath, 404, `{"message":"Not Found"}`),
		get(issuePath, 200, issueJSON(12, "open", "")))
	_, found, err := c.ReadParent(context.Background(), 12)
	if err != nil || found {
		t.Errorf("an issue with no parent read as found=%v err=%v", found, err)
	}
	net.done()

	c, net = readClient(t,
		get(parentPath, 404, `{"message":"Not Found"}`),
		get(issuePath, 404, `{"message":"Not Found"}`))
	_, found, err = c.ReadParent(context.Background(), 12)
	if !errors.Is(err, githubapp.ErrNotFound) || found {
		t.Errorf("a missing issue read as found=%v err=%v", found, err)
	}
	net.done()

	c, _ = readClient(t, get(parentPath, 403, `{"message":"Forbidden"}`))
	if _, _, err = c.ReadParent(context.Background(), 12); !errors.Is(err, githubapp.ErrForbidden) {
		t.Errorf("a refused parent read is reported as %v", err)
	}
}

func TestAnIssueNumberBelowOneNeverReachesTheNetwork(t *testing.T) {
	c, net := readClient(t)
	ctx := context.Background()
	if _, err := c.ReadIssue(ctx, 0); err == nil {
		t.Error("ReadIssue accepted 0")
	}
	if _, err := c.ReadPredecessors(ctx, 0); err == nil {
		t.Error("ReadPredecessors accepted 0")
	}
	if _, _, err := c.ReadParent(ctx, 0); err == nil {
		t.Error("ReadParent accepted 0")
	}
	if _, err := c.ReadPermission(ctx, ""); err == nil {
		t.Error("ReadPermission accepted an empty login")
	}
	net.done()
}

// Issue reads are made with a bare token, which belongs to whoever set it and
// is never spent on anything but a read.
func TestEveryIssueReadOnlyGets(t *testing.T) {
	c, net := readClient(t,
		get(issuePath, 200, issueJSON(12, "open", "")),
		get(permissionPath, 200, `{"permission":"read","role_name":"triage"}`),
		get(issuesPath, 200, issuesJSON(1, 1)),
		get(blockedByPath, 200, `[]`),
		get(parentPath, 404, `{"message":"Not Found"}`),
		get(issuePath, 200, issueJSON(12, "open", "")),
	)
	ctx := context.Background()
	if _, err := c.ReadIssue(ctx, 12); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadPermission(ctx, "octocat"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadLabelledIssues(ctx, "agtk:queued"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadPredecessors(ctx, 12); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.ReadParent(ctx, 12); err != nil {
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
