package tests

import (
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/prwait"
)

const (
	waitHead = "1111111111111111111111111111111111111111"
	waitBase = "2222222222222222222222222222222222222222"
)

// prGitHub is what GitHub answers about o/r#7 for agtk pr wait.
type prGitHub struct {
	state    string
	head     func() string
	runs     string
	statuses string
	comments string
	threads  string
	// viewer is the login GET /user answers for the token.
	viewer string
}

func cleanPR() *prGitHub {
	return &prGitHub{
		state:    "open",
		head:     func() string { return waitHead },
		runs:     `[{"name":"build","status":"completed","conclusion":"success","html_url":"https://github.com/o/r/runs/1"}]`,
		statuses: `[]`,
		comments: `[]`,
		threads:  `[]`,
		viewer:   "agent-bot",
	}
}

func (g *prGitHub) handle(r *http.Request) (int, string) {
	head := g.head()
	switch r.Method + " " + r.URL.Path {
	case "GET /user":
		return 200, `{"login":"` + g.viewer + `"}`
	case "GET /repos/o/r/pulls/7":
		return 200, `{"number":7,"state":"` + g.state + `","base":{"sha":"` + waitBase + `"},"head":{"sha":"` + head + `"}}`
	case "GET /repos/o/r/commits/" + head + "/check-runs":
		return 200, `{"total_count":` + count(g.runs) + `,"check_runs":` + g.runs + `}`
	case "GET /repos/o/r/commits/" + head + "/status":
		return 200, `{"state":"pending","total_count":` + count(g.statuses) + `,"statuses":` + g.statuses + `}`
	case "GET /repos/o/r/issues/7/comments":
		return 200, g.comments
	case "POST /graphql":
		return 200, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":` + g.threads + `}}}}}`
	}
	return 404, `{"message":"Not Found"}`
}

// count is the length of a JSON array literal.
func count(array string) string {
	var items []json.RawMessage
	_ = json.Unmarshal([]byte(array), &items)
	b, _ := json.Marshal(len(items))
	return string(b)
}

func TestPRWaitOnACleanPullRequestExitsZeroWithTheVerdictOnStdout(t *testing.T) {
	withToken(t)
	fake := useGitHub(t, cleanPR().handle)

	code, stdout, stderr := runBacklog(t, "pr", "wait", "o/r#7")
	if code != prwait.ExitClean {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "o/r#7: clean") || !strings.Contains(stdout, `head: "`+waitHead+`"`) {
		t.Errorf("stdout:\n%s", stdout)
	}
	if stderr != "poll 1: 0 pending, 0 failing, 0 waiting threads, 0 answered\n" {
		t.Errorf("stderr = %q", stderr)
	}
	for i, request := range fake.requests {
		if !strings.HasPrefix(request, "GET ") && request != "POST /graphql" {
			t.Errorf("pr wait made %s, which is not a read", request)
		}
		if fake.bearers[i] != "Bearer the-token" {
			t.Errorf("%s carried %q, not the token from GH_TOKEN", request, fake.bearers[i])
		}
	}
}

func TestPRWaitJSONIsOneObjectWithArraysNeverNull(t *testing.T) {
	withToken(t)
	useGitHub(t, cleanPR().handle)

	code, stdout, stderr := runBacklog(t, "pr", "wait", "o/r#7", "--json")
	if code != prwait.ExitClean {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout)
	}
	if got["verdict"] != "clean" || got["head_sha"] != waitHead || got["polls"] != float64(1) || got["referral_pending"] != false {
		t.Errorf("got %v", got)
	}
	if _, ok := got["elapsed_seconds"].(float64); !ok {
		t.Errorf("elapsed_seconds = %v", got["elapsed_seconds"])
	}
	if got["answered_unresolved"] != float64(0) {
		t.Errorf("answered_unresolved = %v, want 0", got["answered_unresolved"])
	}
	for _, key := range []string{"failing", "pending", "waiting_threads"} {
		if items, ok := got[key].([]any); !ok || len(items) != 0 {
			t.Errorf("%s = %v, want []", key, got[key])
		}
	}
}

func TestPRWaitNamesAFailedCheckQuotedAndExitsActionNeeded(t *testing.T) {
	withToken(t)
	pr := cleanPR()
	pr.runs = `[{"name":"evil\nname","status":"completed","conclusion":"failure","html_url":"https://github.com/o/r/runs/2"}]`
	useGitHub(t, pr.handle)

	code, stdout, stderr := runBacklog(t, "pr", "wait", "o/r#7")
	if code != prwait.ExitActionNeeded {
		t.Fatalf("exit %d, want %d; stderr:\n%s", code, prwait.ExitActionNeeded, stderr)
	}
	if !strings.Contains(stdout, `check-run "evil\nname": "failure" "https://github.com/o/r/runs/2"`) {
		t.Errorf("the failing run is not named quoted:\n%s", stdout)
	}
}

func TestPRWaitReportsAWaitingThreadAndTheReferralDetailInJSON(t *testing.T) {
	withToken(t)
	pr := cleanPR()
	pr.statuses = `[{"context":"lydite/referral","state":"pending"}]`
	pr.threads = `[{"path":"a.go","isResolved":false,"isOutdated":false,"comments":{"nodes":[{"body":"please fix","viewerDidAuthor":false}]}}]`
	pr.comments = `[{"id":5,"user":{"login":"lydite[bot]"},"body":"<!-- lydite:results -->\n### 🟡 Needs attention\n<summary>🟡 <b>referral</b></summary>\n| 🟡 | nosec | added |","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]`
	useGitHub(t, pr.handle)

	code, stdout, stderr := runBacklog(t, "pr", "wait", "o/r#7", "--json")
	if code != prwait.ExitActionNeeded {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	var got struct {
		Verdict         string               `json:"verdict"`
		WaitingThreads  []prwait.Thread      `json:"waiting_threads"`
		ReferralPending bool                 `json:"referral_pending"`
		Lydite          *prwait.LyditeDetail `json:"lydite"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if got.Verdict != "action-needed" || len(got.WaitingThreads) != 1 || got.WaitingThreads[0].Path != "a.go" || !got.ReferralPending {
		t.Errorf("got %+v", got)
	}
	if got.Lydite == nil || got.Lydite.Verdict != "referral" || len(got.Lydite.Rows) != 1 {
		t.Errorf("lydite = %+v", got.Lydite)
	}
}

func TestPRWaitCountsAThreadTheTokensAccountAnsweredAndListsOnlyTheWaitingOne(t *testing.T) {
	withToken(t)
	pr := cleanPR()
	pr.threads = `[` +
		`{"path":"answered.go","isResolved":false,"isOutdated":true,"comments":{"nodes":[{"body":"fix this","viewerDidAuthor":false}]},` +
		`"latest":{"nodes":[{"author":{"login":"agent-bot"}}]}},` +
		`{"path":"waiting.go","isResolved":false,"isOutdated":false,"comments":{"nodes":[{"body":"and this","viewerDidAuthor":false}]},` +
		`"latest":{"nodes":[{"author":{"login":"alice"}}]}}]`
	useGitHub(t, pr.handle)

	code, stdout, stderr := runBacklog(t, "pr", "wait", "o/r#7", "--json")
	if code != prwait.ExitActionNeeded {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	var got struct {
		WaitingThreads     []prwait.Thread `json:"waiting_threads"`
		AnsweredUnresolved int             `json:"answered_unresolved"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(got.WaitingThreads) != 1 || got.WaitingThreads[0].Path != "waiting.go" || got.AnsweredUnresolved != 1 {
		t.Errorf("got %+v", got)
	}

	pr.threads = `[{"path":"answered.go","isResolved":false,"isOutdated":false,"comments":{"nodes":[{"body":"fix this","viewerDidAuthor":false}]},` +
		`"latest":{"nodes":[{"author":{"login":"agent-bot"}}]}}]`
	code, stdout, stderr = runBacklog(t, "pr", "wait", "o/r#7")
	if code != prwait.ExitClean {
		t.Fatalf("a pull request whose only open thread is answered: exit %d, stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "answered unresolved threads: 1") || !strings.Contains(stderr, "0 waiting threads, 1 answered") {
		t.Errorf("the answered thread is not reported:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestPRWaitExitsNoChecksWhenTheHeadIsStillBareOnTheSecondPoll(t *testing.T) {
	withToken(t)
	pr := cleanPR()
	pr.runs = `[]`
	useGitHub(t, pr.handle)

	code, stdout, stderr := runBacklog(t, "pr", "wait", "o/r#7", "--poll", "1ms", "--cap", "1m", "--json")
	if code != prwait.ExitNoChecks {
		t.Fatalf("exit %d, want %d; stderr:\n%s", code, prwait.ExitNoChecks, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if got["verdict"] != "no-checks" || got["polls"] != float64(2) {
		t.Errorf("got %v", got)
	}
	if n := strings.Count(stderr, "poll "); n != 2 {
		t.Errorf("%d polls reported, want 2:\n%s", n, stderr)
	}
}

func TestPRWaitOnAClosedPullRequestExitsClosed(t *testing.T) {
	withToken(t)
	pr := cleanPR()
	pr.state = "closed"
	useGitHub(t, pr.handle)

	code, stdout, stderr := runBacklog(t, "pr", "wait", "o/r#7")
	if code != prwait.ExitClosed || !strings.Contains(stdout, `state: "closed"`) {
		t.Errorf("exit %d, stdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if stderr != "poll 1: the pull request is \"closed\"\n" {
		t.Errorf("stderr = %q, want the closing poll's line", stderr)
	}
}

func TestPRWaitExitsHeadMovedWhenAPushLandsMidWait(t *testing.T) {
	withToken(t)
	var reads atomic.Int32
	pr := cleanPR()
	pr.runs = `[{"name":"build","status":"in_progress"}]`
	pr.head = func() string {
		if reads.Load() > 1 {
			return strings.Repeat("3", 40)
		}
		return waitHead
	}
	useGitHub(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/repos/o/r/pulls/7" {
			reads.Add(1)
		}
		return pr.handle(r)
	})

	code, stdout, stderr := runBacklog(t, "pr", "wait", "o/r#7", "--poll", "1ms", "--cap", "1m")
	if code != prwait.ExitHeadMoved || !strings.Contains(stdout, `new head: "`+strings.Repeat("3", 40)+`"`) {
		t.Errorf("exit %d, stdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	want := "poll 1: 1 pending, 0 failing, 0 waiting threads, 0 answered\n" +
		"poll 2: the head moved to \"" + strings.Repeat("3", 40) + "\"\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// lyditeRedPR is a pull request whose failing lydite status asks for the
// detail of a marker comment by author.
func lyditeRedPR(author string) *prGitHub {
	pr := cleanPR()
	pr.statuses = `[{"context":"lydite/test","state":"failure"}]`
	pr.comments = `[{"id":5,"user":{"login":"` + author + `"},"body":"<!-- lydite:results -->\n### ❌ Failing\n<summary>❌ <b>test</b></summary>\n| ❌ | go test | rewrite main.go |","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]`
	return pr
}

func prWaitLydite(t *testing.T, args ...string) *prwait.LyditeDetail {
	t.Helper()
	code, stdout, stderr := runBacklog(t, append([]string{"pr", "wait", "o/r#7", "--json"}, args...)...)
	if code != prwait.ExitActionNeeded {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	var got struct {
		Lydite *prwait.LyditeDetail `json:"lydite"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	return got.Lydite
}

func TestPRWaitReadsTheLyditeCommentOnlyFromTheLyditeAppsBot(t *testing.T) {
	for _, author := range []string{"someapp[bot]", "github-actions[bot]"} {
		withToken(t)
		useGitHub(t, lyditeRedPR(author).handle)
		if l := prWaitLydite(t); l != nil {
			t.Errorf("a marker comment by %s was reported as lydite's: %+v", author, l)
		}
	}
	withToken(t)
	useGitHub(t, lyditeRedPR("lydite[bot]").handle)
	if l := prWaitLydite(t); l == nil || l.Verdict != "red" || len(l.Rows) != 1 {
		t.Errorf("lydite[bot]'s comment: lydite = %+v", l)
	}
}

func TestPRWaitWithLyditeAppReadsOnlyThatAppsBot(t *testing.T) {
	withToken(t)
	useGitHub(t, lyditeRedPR("lydite[bot]").handle)
	if l := prWaitLydite(t, "--lydite-app", "other"); l != nil {
		t.Errorf("lydite[bot] was read with --lydite-app other: %+v", l)
	}
	withToken(t)
	useGitHub(t, lyditeRedPR("other[bot]").handle)
	if l := prWaitLydite(t, "--lydite-app", "other"); l == nil || l.Verdict != "red" {
		t.Errorf("other[bot] was not read with --lydite-app other: %+v", l)
	}
}

func TestPRWaitExitsCapReachedListingWhatIsStillPending(t *testing.T) {
	withToken(t)
	pr := cleanPR()
	pr.runs = `[{"name":"slow","status":"queued"}]`
	useGitHub(t, pr.handle)

	code, stdout, stderr := runBacklog(t, "pr", "wait", "o/r#7", "--poll", "1ms", "--cap", "5ms")
	if code != prwait.ExitCapReached {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "cap-reached") || !strings.Contains(stdout, `check-run "slow": "queued"`) {
		t.Errorf("stdout:\n%s", stdout)
	}
}

func TestPRWaitExitsCouldNotReadAfterThreeFailedPolls(t *testing.T) {
	withToken(t)
	useGitHub(t, func(r *http.Request) (int, string) {
		return 404, `{"message":"Not Found"}`
	})

	code, _, stderr := runBacklog(t, "pr", "wait", "o/r#7", "--poll", "1ms", "--cap", "1m")
	if code != prwait.ExitCouldNotRead {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if n := strings.Count(stderr, "could not read"); n != 3 {
		t.Errorf("%d failed polls reported, want 3:\n%s", n, stderr)
	}
}

func TestPRWaitWithoutATokenExitsCouldNotReadNamingBothVariables(t *testing.T) {
	isolatedConfig(t)
	fake := useGitHub(t, cleanPR().handle)

	code, _, stderr := runBacklog(t, "pr", "wait", "o/r#7")
	if code != prwait.ExitCouldNotRead {
		t.Errorf("exit %d, want %d", code, prwait.ExitCouldNotRead)
	}
	if !strings.Contains(stderr, "GH_TOKEN") || !strings.Contains(stderr, "GITHUB_TOKEN") {
		t.Errorf("stderr does not name GH_TOKEN and GITHUB_TOKEN:\n%s", stderr)
	}
	if len(fake.requests) != 0 {
		t.Errorf("requests made with no token: %v", fake.requests)
	}
}

func TestPRWaitRefusesAMalformedPullRequestWithExitOne(t *testing.T) {
	withToken(t)
	fake := useGitHub(t, cleanPR().handle)
	for _, arg := range []string{"o/r", "o/r#0", "o/r#x", "o#7", "o/r/x#7", "o/r#-1"} {
		if code, _, stderr := runBacklog(t, "pr", "wait", arg); code != 1 || !strings.Contains(stderr, "not a pull request") {
			t.Errorf("%q: exit %d, stderr:\n%s", arg, code, stderr)
		}
	}
	for _, flags := range [][]string{{"--poll", "0s"}, {"--cap", "0s"}} {
		if code, _, _ := runBacklog(t, append([]string{"pr", "wait", "o/r#7"}, flags...)...); code != 1 {
			t.Errorf("%s %s: exit %d, want 1", flags[0], flags[1], code)
		}
	}
	if len(fake.requests) != 0 {
		t.Errorf("requests made for a malformed argument: %v", fake.requests)
	}
}

// elapsedRE matches the rounded elapsed time in the text verdict, the one part
// of it that depends on how long the run took.
var elapsedRE = regexp.MustCompile(`elapsed: [0-9hms]+\n`)

// verdictBlock is stdout with its elapsed time written as "elapsed: E".
func verdictBlock(stdout string) string {
	return elapsedRE.ReplaceAllString(stdout, "elapsed: E\n")
}

func TestPRWaitTextVerdictOnACleanPullRequestIsTheHeaderAlone(t *testing.T) {
	withToken(t)
	useGitHub(t, cleanPR().handle)

	code, stdout, stderr := runBacklog(t, "pr", "wait", "o/r#7")
	if code != prwait.ExitClean {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	want := "o/r#7: clean\n" +
		`head: "` + waitHead + `"` + "\n" +
		"polls: 1, elapsed: E\n"
	if got := verdictBlock(stdout); got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestPRWaitTextVerdictListsFailingPendingAndWaitingUnderTheirHeadings(t *testing.T) {
	withToken(t)
	pr := cleanPR()
	pr.runs = `[{"name":"evil\nname","status":"completed","conclusion":"failure","html_url":"https://github.com/o/r/runs/2"},` +
		`{"name":"slow","status":"queued","html_url":"https://github.com/o/r/runs/3"}]`
	pr.threads = `[{"path":"a.go","isResolved":false,"isOutdated":true,"comments":{"nodes":[{"body":"please fix","viewerDidAuthor":false}]},` +
		`"latest":{"nodes":[{"author":{"login":"alice"}}]}}]`
	useGitHub(t, pr.handle)

	code, stdout, stderr := runBacklog(t, "pr", "wait", "o/r#7")
	if code != prwait.ExitActionNeeded {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	want := "o/r#7: action-needed\n" +
		`head: "` + waitHead + `"` + "\n" +
		"polls: 1, elapsed: E\n" +
		"failing:\n" +
		`  check-run "evil\nname": "failure" "https://github.com/o/r/runs/2"` + "\n" +
		"pending:\n" +
		`  check-run "slow": "queued" "https://github.com/o/r/runs/3"` + "\n" +
		"waiting threads:\n" +
		`  "a.go" (outdated): "please fix"` + "\n"
	if got := verdictBlock(stdout); got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestPRWaitTextVerdictNamesTheReadErrorQuoted(t *testing.T) {
	withToken(t)
	useGitHub(t, func(r *http.Request) (int, string) {
		return 404, `{"message":"Not Found"}`
	})

	code, stdout, stderr := runBacklog(t, "pr", "wait", "o/r#7", "--poll", "1ms", "--cap", "1m")
	if code != prwait.ExitCouldNotRead {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if !regexp.MustCompile(`(?m)^polls: 3, elapsed: [0-9hms]+\nerror: ".+"$`).MatchString(stdout) {
		t.Errorf("stdout does not name the polls and the read error:\n%s", stdout)
	}
}

func TestPRWaitJSONNamesEveryFailingAndPendingItem(t *testing.T) {
	withToken(t)
	pr := cleanPR()
	pr.runs = `[{"name":"build","status":"completed","conclusion":"failure","html_url":"https://github.com/o/r/runs/2"},` +
		`{"name":"slow","status":"queued","html_url":"https://github.com/o/r/runs/3"}]`
	useGitHub(t, pr.handle)

	code, stdout, stderr := runBacklog(t, "pr", "wait", "o/r#7", "--json")
	if code != prwait.ExitActionNeeded {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	var got struct {
		Failing []prwait.Item `json:"failing"`
		Pending []prwait.Item `json:"pending"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(got.Failing) != 1 || got.Failing[0].Name != "build" {
		t.Errorf("failing = %+v, want build", got.Failing)
	}
	if len(got.Pending) != 1 || got.Pending[0].Name != "slow" {
		t.Errorf("pending = %+v, want slow", got.Pending)
	}
}

func TestPRWaitJSONElapsedIsWholeSecondsOfTheRun(t *testing.T) {
	withToken(t)
	useGitHub(t, cleanPR().handle)

	start := time.Now()
	code, stdout, stderr := runBacklog(t, "pr", "wait", "o/r#7", "--json")
	took := time.Since(start)
	if code != prwait.ExitClean {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	var got struct {
		ElapsedSeconds int64 `json:"elapsed_seconds"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if limit := int64(math.Ceil(took.Seconds())); got.ElapsedSeconds < 0 || got.ElapsedSeconds > limit {
		t.Errorf("elapsed_seconds = %d for a run that took %s", got.ElapsedSeconds, took)
	}
}
