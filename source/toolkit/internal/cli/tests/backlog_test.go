package tests

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/backlog"
	"github.com/pedromvgomes/agentic-toolkit/internal/cli"
)

// fakeGitHub answers the requests agtk backlog makes, in place of the
// network. handle returns the status and body for one request.
type fakeGitHub struct {
	t      *testing.T
	handle func(r *http.Request) (int, string)

	mu       sync.Mutex
	requests []string
	bearers  []string
}

func (f *fakeGitHub) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
	f.bearers = append(f.bearers, r.Header.Get("Authorization"))
	f.mu.Unlock()
	if r.URL.Host != "api.github.com" {
		f.t.Errorf("request to %s, which is not GitHub's API", r.URL)
	}
	status, body := f.handle(r)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}, nil
}

// useGitHub routes the process's default transport, which every GitHub
// client agtk builds without a transport of its own reaches the network
// through, to handle for the life of the test. The tests that call it set
// environment variables and so never run in parallel with another test.
func useGitHub(t *testing.T, handle func(r *http.Request) (int, string)) *fakeGitHub {
	t.Helper()
	fake := &fakeGitHub{t: t, handle: handle}
	previous := http.DefaultTransport
	http.DefaultTransport = fake
	t.Cleanup(func() { http.DefaultTransport = previous })
	return fake
}

// withToken isolates the configuration and environment and sets GH_TOKEN.
func withToken(t *testing.T) string {
	t.Helper()
	config := isolatedConfig(t)
	t.Setenv("GH_TOKEN", "the-token")
	return config
}

// runBacklog runs agtk with args through the same exit-code mapping the
// binary uses.
func runBacklog(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	env := &cli.Env{Stdin: strings.NewReader(""), Stdout: &outBuf, Stderr: &errBuf, WorkDir: t.TempDir()}
	code = cli.ExecuteArgs(env, args)
	return code, outBuf.String(), errBuf.String()
}

// restIssue is an issue as GitHub's REST API answers it.
func restIssue(number int, title, state, reason string, labels ...string) string {
	named := make([]map[string]string, 0, len(labels))
	for _, label := range labels {
		named = append(named, map[string]string{"name": label})
	}
	raw, _ := json.Marshal(map[string]any{
		"number": number, "title": title, "state": state, "state_reason": reason,
		"user": map[string]string{"login": "alice"}, "labels": named,
	})
	return string(raw)
}

// trustAnswer is the GraphQL answer for an issue authored by alice and
// queued by alice, with no body edits.
func trustAnswer(number int, body string) string {
	raw, _ := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{"issue": map[string]any{
		"number":       number,
		"body":         body,
		"lastEditedAt": nil,
		"author":       map[string]string{"__typename": "User", "login": "alice"},
		"userContentEdits": map[string]any{
			"totalCount": 0, "pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""}, "nodes": []any{},
		},
		"timelineItems": map[string]any{
			"totalCount": 1, "pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
			"nodes": []any{map[string]any{
				"__typename": "LabeledEvent", "createdAt": "2026-01-01T00:00:00Z",
				"actor": map[string]string{"__typename": "User", "login": "alice"},
				"label": map[string]string{"name": backlog.LabelQueued},
			}},
		},
	}}}})
	return string(raw)
}

// trustedBody is a body carrying what a raw print must keep: a line of its
// own, a terminal escape, and no trailing newline.
const trustedBody = "## Goal\nDo the thing.\n\x1b[31mred\x1b[0m\r\nverify: go test ./..."

// trustedItem answers every read of o/r#7 as a trusted, queued item.
func trustedItem(r *http.Request) (int, string) {
	switch r.Method + " " + r.URL.Path {
	case "GET /repos/o/r/issues/7":
		return 200, restIssue(7, "a title", "open", "", backlog.LabelQueued)
	case "POST /graphql":
		return 200, trustAnswer(7, trustedBody)
	case "GET /repos/o/r/collaborators/alice/permission":
		return 200, `{"permission":"write","role_name":"write"}`
	}
	return 404, `{"message":"Not Found"}`
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestVerifyPrintsTheHashLineThenTheBodyByteForByte(t *testing.T) {
	withToken(t)
	fake := useGitHub(t, trustedItem)

	code, stdout, stderr := runBacklog(t, "backlog", "verify", "o/r#7")
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	want := "sha256 " + sha(trustedBody) + "\n" + trustedBody
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	for _, bearer := range fake.bearers {
		if bearer != "Bearer the-token" {
			t.Errorf("a request carried %q, not the token from GH_TOKEN", bearer)
		}
	}
}

func TestVerifyJSONCarriesRepoNumberHashAndBody(t *testing.T) {
	withToken(t)
	useGitHub(t, trustedItem)

	code, stdout, stderr := runBacklog(t, "backlog", "verify", "o/r#7", "--json")
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	want := map[string]any{"repo": "o/r", "number": float64(7), "sha256": sha(trustedBody), "body": trustedBody}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("verify --json = %v, want %v", got, want)
	}
}

func TestVerifyReadsWithGITHUB_TOKENWhenGH_TOKENIsUnset(t *testing.T) {
	isolatedConfig(t)
	t.Setenv("GITHUB_TOKEN", "the-other-token")
	fake := useGitHub(t, trustedItem)

	if code, _, stderr := runBacklog(t, "backlog", "verify", "o/r#7"); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	for _, bearer := range fake.bearers {
		if bearer != "Bearer the-other-token" {
			t.Errorf("a request carried %q, not the token from GITHUB_TOKEN", bearer)
		}
	}
}

// ADR 0028: verify reads with a token even on a machine registered as the
// code-review App, so the only requests made are the item's own reads, and
// none asks GitHub for an installation token.
func TestVerifyNeverReadsAsTheAppRegistration(t *testing.T) {
	isolatedConfig(t)
	work := t.TempDir()
	keyFile := filepath.Join(work, "app.pem")
	if err := os.WriteFile(keyFile, []byte(keyPEM(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := runCLI(t, work, "code-review", "initialize", "--app-id", "4242", "--key-file", keyFile); err != nil {
		t.Fatalf("initialize: %v\n%s", err, stderr)
	}
	t.Setenv("GH_TOKEN", "the-token")
	fake := useGitHub(t, trustedItem)

	if code, _, stderr := runBacklog(t, "backlog", "verify", "o/r#7"); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	for i, request := range fake.requests {
		if strings.Contains(request, "/app/") || strings.Contains(request, "/installation") {
			t.Errorf("verify made %s, a request only the App's client makes", request)
		}
		if fake.bearers[i] != "Bearer the-token" {
			t.Errorf("%s carried %q, not the token", request, fake.bearers[i])
		}
	}
}

// An item the named App queued is trusted only when --trusted-app names it:
// a bot's login is no collaborator, so without the flag nobody vouches for it.
func TestVerifyTrustsTheLabelOfTheAppNamedByTrustedApp(t *testing.T) {
	withToken(t)
	queuedByApp := strings.Replace(trustAnswer(7, trustedBody),
		`"actor":{"__typename":"User","login":"alice"}`, `"actor":{"__typename":"Bot","login":"factory"}`, 1)
	if queuedByApp == trustAnswer(7, trustedBody) {
		t.Fatal("the trust answer names no label actor to replace")
	}
	useGitHub(t, func(r *http.Request) (int, string) {
		if r.Method+" "+r.URL.Path == "POST /graphql" {
			return 200, queuedByApp
		}
		return trustedItem(r)
	})

	code, stdout, stderr := runBacklog(t, "backlog", "verify", "o/r#7", "--trusted-app", "factory")
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if want := "sha256 " + sha(trustedBody) + "\n" + trustedBody; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}

	code, _, stderr = runBacklog(t, "backlog", "verify", "o/r#7")
	if code != 1 || !strings.Contains(stderr, string(backlog.ReasonLabelActorUntrusted)) {
		t.Errorf("without --trusted-app, exit %d, stderr:\n%s", code, stderr)
	}
}

func TestANotTrustedItemExitsNonZeroAndNamesTheReasonOnStderr(t *testing.T) {
	withToken(t)
	useGitHub(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/repos/o/r/issues/7" {
			return 200, restIssue(7, "a title", "open", "")
		}
		return 404, `{"message":"Not Found"}`
	})

	code, stdout, stderr := runBacklog(t, "backlog", "verify", "o/r#7")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("a refused item printed %q on stdout", stdout)
	}
	for _, want := range []string{"o/r#7", "not trusted", string(backlog.ReasonNotQueued)} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, "could not determine") {
		t.Errorf("a failed condition reads as a failed read:\n%s", stderr)
	}
}

func TestAnUnreadableItemExitsNonZeroAsCouldNotDetermine(t *testing.T) {
	withToken(t)
	useGitHub(t, func(r *http.Request) (int, string) {
		return 502, `{"message":"Bad Gateway"}`
	})

	code, stdout, stderr := runBacklog(t, "backlog", "verify", "o/r#7")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("an unreadable item printed %q on stdout", stdout)
	}
	for _, want := range []string{"o/r#7", "could not determine", string(backlog.ReasonIssueUnreadable)} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, "not trusted") {
		t.Errorf("a failed read reads as a failed condition:\n%s", stderr)
	}
}

func TestARefusalWithJSONIsMachineReadableOnStdout(t *testing.T) {
	for name, tc := range map[string]struct {
		handle func(r *http.Request) (int, string)
		kind   backlog.Kind
		reason backlog.Reason
	}{
		"not trusted": {
			handle: func(r *http.Request) (int, string) {
				return 200, restIssue(7, "a title", "closed", "completed", backlog.LabelQueued)
			},
			kind: backlog.Untrusted, reason: backlog.ReasonClosed,
		},
		"could not determine": {
			handle: func(r *http.Request) (int, string) { return 500, `{"message":"boom"}` },
			kind:   backlog.Undetermined, reason: backlog.ReasonIssueUnreadable,
		},
	} {
		t.Run(name, func(t *testing.T) {
			withToken(t)
			useGitHub(t, tc.handle)

			code, stdout, stderr := runBacklog(t, "backlog", "verify", "o/r#7", "--json")
			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			var got struct {
				Repo    string `json:"repo"`
				Number  int    `json:"number"`
				Kind    string `json:"kind"`
				Reason  string `json:"reason"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
			}
			if got.Repo != "o/r" || got.Number != 7 || got.Kind != string(tc.kind) || got.Reason != string(tc.reason) || got.Message == "" {
				t.Errorf("refusal = %+v, want o/r#7 %s %s with a message", got, tc.kind, tc.reason)
			}
			if !strings.Contains(stderr, string(tc.reason)) {
				t.Errorf("stderr does not name the reason:\n%s", stderr)
			}
		})
	}
}

func TestARefusalMessageIsQuotedOnStderr(t *testing.T) {
	withToken(t)
	useGitHub(t, func(r *http.Request) (int, string) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/o/r/issues/7":
			return 200, restIssue(7, "a title", "open", "", backlog.LabelQueued)
		case "POST /graphql":
			answer := strings.Replace(trustAnswer(7, "body"), `"login":"alice"}`, `"login":"mallory\nINJECTED"}`, 1)
			return 200, answer
		}
		if strings.HasSuffix(r.URL.Path, "/alice/permission") {
			return 200, `{"permission":"write","role_name":"write"}`
		}
		return 200, `{"permission":"read","role_name":"read"}`
	})

	code, _, stderr := runBacklog(t, "backlog", "verify", "o/r#7")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(stderr, string(backlog.ReasonAuthorLacksWrite)) {
		t.Fatalf("stderr does not name %s:\n%s", backlog.ReasonAuthorLacksWrite, stderr)
	}
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, "INJECTED") {
			t.Errorf("a login from GitHub started a line of its own on stderr:\n%s", stderr)
		}
	}
}

func TestBacklogCommandsWithoutATokenFailNamingBothVariables(t *testing.T) {
	isolatedConfig(t)
	fake := useGitHub(t, trustedItem)

	for _, args := range [][]string{
		{"backlog", "verify", "o/r#7"},
		{"backlog", "list", "--repo", "o/r"},
	} {
		code, stdout, stderr := runBacklog(t, args...)
		if code == 0 {
			t.Errorf("%v exited 0 with no token", args)
		}
		if stdout != "" {
			t.Errorf("%v printed %q on stdout", args, stdout)
		}
		if !strings.Contains(stderr, "GH_TOKEN") || !strings.Contains(stderr, "GITHUB_TOKEN") {
			t.Errorf("%v: stderr does not name GH_TOKEN and GITHUB_TOKEN:\n%s", args, stderr)
		}
	}
	if len(fake.requests) != 0 {
		t.Errorf("requests made with no token: %v", fake.requests)
	}
}

func TestVerifyRefusesAMalformedItem(t *testing.T) {
	withToken(t)
	fake := useGitHub(t, trustedItem)

	for _, arg := range []string{
		"o/r", "o/r#", "o/r#0", "o/r#-1", "o/r#+1", "o/r#1x", "/r#1", "o/#1", "o#1", "o/r/x#1",
		"o/r#99999999999999999999999", "o/..#1", "o r/x#1",
	} {
		if code, _, _ := runBacklog(t, "backlog", "verify", arg); code == 0 {
			t.Errorf("verify %q exited 0", arg)
		}
	}
	if code, _, _ := runBacklog(t, "backlog", "verify", "o/r#7", "--trusted-app", "factory[bot]"); code == 0 {
		t.Errorf("verify accepted a --trusted-app that is not a slug")
	}
	if len(fake.requests) != 0 {
		t.Errorf("requests made for a malformed item: %v", fake.requests)
	}
}

func TestListRefusesAMalformedRepo(t *testing.T) {
	withToken(t)
	fake := useGitHub(t, trustedItem)

	for _, repo := range []string{"", "o", "o/", "/r", "o/r/x", "o/r#1"} {
		if code, _, _ := runBacklog(t, "backlog", "list", "--repo", repo); code == 0 {
			t.Errorf("list --repo %q exited 0", repo)
		}
	}
	if code, _, _ := runBacklog(t, "backlog", "list"); code == 0 {
		t.Errorf("list without --repo exited 0")
	}
	if len(fake.requests) != 0 {
		t.Errorf("requests made for a malformed repo: %v", fake.requests)
	}
}

// backlogOnGitHub is a repository with three items: #3, queued with a
// newline in its title, blocked by #1 (done) and #2 (not planned), in
// Feature #9; #4, a stub with no state; #5, blocked, with no predecessors
// and no Feature.
func backlogOnGitHub(r *http.Request) (int, string) {
	path := r.URL.Path
	switch {
	case path == "/repos/o/r/issues" && r.URL.Query().Get("page") == "1":
		switch r.URL.Query().Get("labels") {
		case backlog.LabelQueued:
			return 200, "[" + restIssue(3, "line one\nqueued: yes", "open", "", backlog.LabelQueued) + "]"
		case backlog.LabelStub:
			return 200, "[" + restIssue(4, "a stub", "open", "", backlog.LabelStub) + "]"
		case backlog.LabelBlocked:
			return 200, "[" + restIssue(5, "waiting", "open", "", backlog.LabelBlocked) + "]"
		}
		return 200, "[]"
	case path == "/repos/o/r/issues/3/dependencies/blocked_by":
		return 200, "[" + restIssue(1, "one", "closed", "completed") + "," + restIssue(2, "two", "closed", "not_planned") + "]"
	case strings.HasSuffix(path, "/dependencies/blocked_by"):
		return 200, "[]"
	case path == "/repos/o/r/issues/3/parent":
		return 200, restIssue(9, "the \"feature\"", "open", "")
	case path == "/repos/o/r/issues/4/parent", path == "/repos/o/r/issues/5/parent":
		return 404, `{"message":"Not Found"}`
	}
	return 404, `{"message":"Not Found"}`
}

func TestListPrintsEachItemsStatesStubPredecessorsAndFeatureWithQuotedTitles(t *testing.T) {
	withToken(t)
	fake := useGitHub(t, backlogOnGitHub)

	code, stdout, stderr := runBacklog(t, "backlog", "list", "--repo", "o/r")
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	want := `#3 "line one\nqueued: yes" | states: "agtk:queued" | stub: no | predecessors: #1 #2, not all done | feature: #9 "the \"feature\""` + "\n" +
		`#4 "a stub" | states: none | stub: yes | predecessors: none | feature: none` + "\n" +
		`#5 "waiting" | states: "agtk:blocked" | stub: no | predecessors: none | feature: none` + "\n"
	if stdout != want {
		t.Errorf("list =\n%s\nwant\n%s", stdout, want)
	}
	for _, word := range []string{"ready", "trusted", "verified"} {
		if strings.Contains(strings.ToLower(stdout), word) {
			t.Errorf("list output says %q, which only verify may claim", word)
		}
	}
	// An item with no parent answers 404 on its parent read; the label scan
	// already showed it visible, so it is not read again to tell the two
	// 404s apart.
	for _, request := range fake.requests {
		if request == "GET /repos/o/r/issues/4" || request == "GET /repos/o/r/issues/5" {
			t.Errorf("list made %s for an item its label scan returned", request)
		}
	}
}

func TestListJSONIsAnArrayOfItemsWithArraysNeverNull(t *testing.T) {
	withToken(t)
	useGitHub(t, backlogOnGitHub)

	code, stdout, stderr := runBacklog(t, "backlog", "list", "--repo", "o/r", "--json")
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if strings.Contains(stdout, "null") && !strings.Contains(stdout, `"feature": null`) {
		t.Errorf("list --json carries a null array:\n%s", stdout)
	}
	var items []backlog.Item
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("stdout is not a JSON array of items: %v\n%s", err, stdout)
	}
	if len(items) != 3 || items[0].Number != 3 || items[1].Number != 4 || items[2].Number != 5 {
		t.Fatalf("items = %+v, want #3, #4, #5", items)
	}
	if items[0].Title != "line one\nqueued: yes" || items[0].PredecessorsDone || items[0].Feature == nil || items[0].Feature.Number != 9 {
		t.Errorf("item #3 = %+v", items[0])
	}
	if !items[1].Stub || len(items[1].States) != 0 {
		t.Errorf("item #4 = %+v, want a stub in no state", items[1])
	}
	if !strings.Contains(stdout, `"states": []`) || !strings.Contains(stdout, `"predecessors": []`) {
		t.Errorf("empty states and predecessors are not encoded as []:\n%s", stdout)
	}
}

func TestAnEmptyBacklogListsAsAnEmptyJSONArray(t *testing.T) {
	withToken(t)
	useGitHub(t, func(r *http.Request) (int, string) { return 200, "[]" })

	code, stdout, stderr := runBacklog(t, "backlog", "list", "--repo", "o/r", "--json")
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Errorf("list --json of an empty backlog = %q, want []", stdout)
	}

	code, stdout, _ = runBacklog(t, "backlog", "list", "--repo", "o/r")
	if code != 0 || stdout != "no open backlog items on o/r\n" {
		t.Errorf("list of an empty backlog = %d %q", code, stdout)
	}
}

func TestAFailedListReadExitsNonZero(t *testing.T) {
	withToken(t)
	useGitHub(t, func(r *http.Request) (int, string) { return 500, `{"message":"boom"}` })

	code, stdout, _ := runBacklog(t, "backlog", "list", "--repo", "o/r", "--json")
	if code == 0 {
		t.Errorf("a failed read exited 0")
	}
	if stdout != "" {
		t.Errorf("a failed list printed %q", stdout)
	}
}

func TestLabelsPrintsNameColourAndDescriptionOfEachCanonicalLabel(t *testing.T) {
	isolatedConfig(t)
	fake := useGitHub(t, trustedItem)

	code, stdout, stderr := runBacklog(t, "backlog", "labels")
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	var want strings.Builder
	for _, label := range backlog.Labels() {
		fmt.Fprintf(&want, "%q %s %q\n", label.Name, label.Color, label.Description)
	}
	if stdout != want.String() {
		t.Errorf("labels =\n%s\nwant\n%s", stdout, want.String())
	}
	if len(fake.requests) != 0 {
		t.Errorf("labels made requests: %v", fake.requests)
	}
}

func TestLabelsJSONIsAnArrayOfNameColourDescription(t *testing.T) {
	code, stdout, stderr := runBacklog(t, "backlog", "labels", "--json")
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	var got []map[string]string
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", err, stdout)
	}
	labels := backlog.Labels()
	if len(got) != len(labels) {
		t.Fatalf("labels --json has %d entries, want %d", len(got), len(labels))
	}
	for i, label := range labels {
		want := map[string]string{"name": label.Name, "color": label.Color, "description": label.Description}
		if fmt.Sprint(got[i]) != fmt.Sprint(want) {
			t.Errorf("entry %d = %v, want %v", i, got[i], want)
		}
	}
}

// TestBacklogRejectsAnUnknownSubcommand: cobra takes an unknown subcommand of
// a non-root parent as an argument, prints help and exits 0, unless the parent
// has a RunE. A caller running a subcommand its installed agtk lacks would
// read the help text as output and report success.
func TestBacklogRejectsAnUnknownSubcommand(t *testing.T) {
	isolatedConfig(t)

	code, stdout, stderr := runBacklog(t, "backlog", "nosuchcommand")

	if code == 0 {
		t.Fatalf("an unknown subcommand exited 0 and printed:\n%s", stdout)
	}
	if !strings.Contains(stderr, "nosuchcommand") {
		t.Errorf("stderr does not name the subcommand:\n%s", stderr)
	}
}
