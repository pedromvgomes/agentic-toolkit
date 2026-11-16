package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

// trustPageLength mirrors the unexported page size ReadIssueTrust asks for:
// GraphQL's own maximum. A connection longer than this is one that pages.
const trustPageLength = 100

// trustActorJSON renders an account as the trust queries read it; nil is a
// null, which is how GraphQL names a deleted account.
func trustActorJSON(login, typ string) any {
	if login == "" && typ == "" {
		return nil
	}
	return map[string]any{"__typename": typ, "login": login}
}

// trustEdit renders one userContentEdits node.
func trustEdit(editor any, editedAt time.Time, deletedAt *time.Time, deletedBy any) map[string]any {
	node := map[string]any{"editedAt": editedAt.Format(time.RFC3339), "editor": editor,
		"deletedAt": nil, "deletedBy": deletedBy}
	if deletedAt != nil {
		node["deletedAt"] = deletedAt.Format(time.RFC3339)
	}
	return node
}

// trustLabelEvent renders one timelineItems node.
func trustLabelEvent(typename, label string, actor any, createdAt time.Time) map[string]any {
	return map[string]any{"__typename": typename, "createdAt": createdAt.Format(time.RFC3339),
		"actor": actor, "label": map[string]any{"name": label}}
}

// trustConnection renders one page of a connection. A nil nodes slice
// renders the size-only selection a page of the other connection carries.
func trustConnection(total int, nodes []map[string]any, hasNext bool, cursor string) map[string]any {
	if nodes == nil {
		return map[string]any{"totalCount": total}
	}
	return map[string]any{"totalCount": total,
		"pageInfo": map[string]any{"hasNextPage": hasNext, "endCursor": cursor}, "nodes": nodes}
}

// trustIssueState is what every page of the read repeats about the issue.
type trustIssueState struct {
	body         string
	lastEditedAt string
	author       any
}

var defaultTrustState = trustIssueState{
	body:         "## Goal\nDo the thing.",
	lastEditedAt: "2026-03-01T10:00:00Z",
	author:       map[string]any{"__typename": "User", "login": "octocat"},
}

// trustAnswer renders one GraphQL answer to a trust query.
func trustAnswer(t *testing.T, state trustIssueState, edits, timeline map[string]any) exchange {
	t.Helper()
	issue := map[string]any{"number": 12, "body": state.body, "lastEditedAt": state.lastEditedAt,
		"author": state.author, "userContentEdits": edits, "timelineItems": timeline}
	raw, err := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{"issue": issue}}})
	if err != nil {
		t.Fatal(err)
	}
	return query(string(raw))
}

// editsRun renders n edits by octocat, a minute apart from start.
func editsRun(start time.Time, n int) []map[string]any {
	out := make([]map[string]any, n)
	for i := range out {
		out[i] = trustEdit(trustActorJSON("octocat", "User"), start.Add(time.Duration(i)*time.Minute), nil, nil)
	}
	return out
}

// labelRun renders n alternating labeled and unlabeled events of agtk:queued.
func labelRun(start time.Time, n int) []map[string]any {
	out := make([]map[string]any, n)
	for i := range out {
		typename := "LabeledEvent"
		if i%2 == 1 {
			typename = "UnlabeledEvent"
		}
		out[i] = trustLabelEvent(typename, "agtk:queued", trustActorJSON("octocat", "User"),
			start.Add(time.Duration(i)*time.Minute))
	}
	return out
}

// sentQuery is the GraphQL request a scripted call carried.
type sentQuery struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// sentQueries decodes every request body the client sent, and fails the test
// when any request was anything but a POST to the GraphQL endpoint or when
// any query could change something.
func sentQueries(t *testing.T, net *scripted) []sentQuery {
	t.Helper()
	out := make([]sentQuery, len(net.bodies))
	for i, req := range net.seen {
		if req.Method != http.MethodPost || req.URL.Path != graphqlPath {
			t.Errorf("call %d was %s %s; reading an issue's trust record calls only POST %s",
				i+1, req.Method, req.URL.Path, graphqlPath)
		}
		if err := json.Unmarshal([]byte(net.bodies[i]), &out[i]); err != nil {
			t.Fatalf("call %d sent a body that is not a GraphQL request: %v", i+1, err)
		}
		if strings.Contains(strings.ToLower(out[i].Query), "mutation") {
			t.Errorf("call %d sent a query naming a mutation:\n%s", i+1, out[i].Query)
		}
	}
	return out
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestAnIssueTrustRecordCarriesItsBodyAuthorEditsAndLabelEventsFromOneQuery(t *testing.T) {
	created := mustTime(t, "2026-03-01T09:00:00Z")
	edited := mustTime(t, "2026-03-01T10:00:00Z")
	deleted := mustTime(t, "2026-03-02T08:00:00Z")
	queued := mustTime(t, "2026-03-03T12:00:00Z")
	edits := []map[string]any{
		trustEdit(trustActorJSON("octocat", "User"), edited, nil, nil),
		trustEdit(nil, created, &deleted, trustActorJSON("hubot", "Bot")),
	}
	events := []map[string]any{
		trustLabelEvent("LabeledEvent", "agtk:queued", trustActorJSON("factory", "Bot"), queued),
		trustLabelEvent("UnlabeledEvent", "agtk:queued", nil, queued.Add(time.Hour)),
	}
	c, net := readClient(t, trustAnswer(t, defaultTrustState,
		trustConnection(2, edits, false, ""), trustConnection(2, events, false, "")))

	got, err := c.ReadIssueTrust(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	sent := sentQueries(t, net)
	if len(sent) != 1 {
		t.Fatalf("the record took %d queries, want one", len(sent))
	}
	for _, field := range []string{"body", "author", "userContentEdits", "editor", "deletedAt", "timelineItems",
		"LABELED_EVENT", "UNLABELED_EVENT", "__typename"} {
		if !strings.Contains(sent[0].Query, field) {
			t.Errorf("the one query does not read %s:\n%s", field, sent[0].Query)
		}
	}
	if sent[0].Variables["owner"] != "acme" || sent[0].Variables["repo"] != "widgets" || sent[0].Variables["number"] != float64(12) {
		t.Errorf("the query addressed %v", sent[0].Variables)
	}

	if got.Number != 12 || got.Body != defaultTrustState.body {
		t.Errorf("read issue %d with body %q", got.Number, got.Body)
	}
	if got.Author == nil || *got.Author != (githubapp.Actor{Login: "octocat", Type: "User"}) {
		t.Errorf("read author %+v", got.Author)
	}
	if len(got.Edits) != 2 {
		t.Fatalf("read %d edits, want 2", len(got.Edits))
	}
	first, second := got.Edits[0], got.Edits[1]
	if first.Editor == nil || first.Editor.Login != "octocat" || !first.EditedAt.Equal(edited) || first.Deleted ||
		!first.DeletedAt.IsZero() || first.DeletedBy != nil {
		t.Errorf("read the first edit as %+v", first)
	}
	if second.Editor != nil || !second.EditedAt.Equal(created) || !second.Deleted || !second.DeletedAt.Equal(deleted) ||
		second.DeletedBy == nil || *second.DeletedBy != (githubapp.Actor{Login: "hubot", Type: "Bot"}) {
		t.Errorf("read the deleted edit by a deleted account as %+v", second)
	}
	if len(got.LabelEvents) != 2 {
		t.Fatalf("read %d label events, want 2", len(got.LabelEvents))
	}
	labeled, unlabeled := got.LabelEvents[0], got.LabelEvents[1]
	if !labeled.Added || labeled.Label != "agtk:queued" || !labeled.CreatedAt.Equal(queued) ||
		labeled.Actor == nil || *labeled.Actor != (githubapp.Actor{Login: "factory", Type: "Bot"}) {
		t.Errorf("read the labeled event as %+v", labeled)
	}
	if unlabeled.Added || unlabeled.Label != "agtk:queued" || unlabeled.Actor != nil ||
		!unlabeled.CreatedAt.Equal(queued.Add(time.Hour)) {
		t.Errorf("read the unlabeled event as %+v", unlabeled)
	}
}

// A deleted account is a null, and must stay distinguishable from an account
// whose login is empty: one names nobody, the other is malformed.
func TestADeletedAuthorAndEditorReadAsNobodyRatherThanAnEmptyLogin(t *testing.T) {
	state := defaultTrustState
	state.author = nil
	edits := []map[string]any{trustEdit(nil, mustTime(t, "2026-03-01T10:00:00Z"), nil, nil)}
	c, _ := readClient(t, trustAnswer(t, state,
		trustConnection(1, edits, false, ""), trustConnection(0, []map[string]any{}, false, "")))

	got, err := c.ReadIssueTrust(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	if got.Author != nil {
		t.Errorf("a deleted author read as %+v", got.Author)
	}
	if len(got.Edits) != 1 || got.Edits[0].Editor != nil {
		t.Errorf("an edit by a deleted account read as %+v", got.Edits)
	}
}

func TestAnEditHistoryLongerThanOnePageIsReadToItsEnd(t *testing.T) {
	start := mustTime(t, "2026-01-01T00:00:00Z")
	total := trustPageLength*2 + 7
	events := labelRun(start, 1)
	c, net := readClient(t,
		trustAnswer(t, defaultTrustState,
			trustConnection(total, editsRun(start, trustPageLength), true, "edits-1"),
			trustConnection(1, events, false, "")),
		trustAnswer(t, defaultTrustState,
			trustConnection(total, editsRun(start.Add(time.Hour*100), trustPageLength), true, "edits-2"),
			trustConnection(1, nil, false, "")),
		trustAnswer(t, defaultTrustState,
			trustConnection(total, editsRun(start.Add(time.Hour*200), 7), false, "edits-3"),
			trustConnection(1, nil, false, "")),
	)

	got, err := c.ReadIssueTrust(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if len(got.Edits) != total {
		t.Fatalf("read %d edits, want %d", len(got.Edits), total)
	}
	if !got.Edits[total-1].EditedAt.Equal(start.Add(time.Hour*200 + 6*time.Minute)) {
		t.Errorf("the last edit read is %+v, not the last page's last", got.Edits[total-1])
	}
	if len(got.LabelEvents) != 1 {
		t.Errorf("read %d label events, want 1", len(got.LabelEvents))
	}
	sent := sentQueries(t, net)
	for i, cursor := range []string{"edits-1", "edits-2"} {
		q := sent[i+1]
		if q.Variables["editsCursor"] != cursor {
			t.Errorf("follow-up %d asked for edits after %v, want %s", i+1, q.Variables["editsCursor"], cursor)
		}
		if _, ok := q.Variables["timelineCursor"]; ok || strings.Contains(q.Query, "LabeledEvent") {
			t.Errorf("follow-up %d for edits also paged label events:\n%s", i+1, q.Query)
		}
	}
}

func TestLabelEventsLongerThanOnePageAreReadToTheirEnd(t *testing.T) {
	start := mustTime(t, "2026-01-01T00:00:00Z")
	total := trustPageLength + 3
	edits := editsRun(start, 1)
	c, net := readClient(t,
		trustAnswer(t, defaultTrustState,
			trustConnection(1, edits, false, ""),
			trustConnection(total, labelRun(start, trustPageLength), true, "timeline-1")),
		trustAnswer(t, defaultTrustState,
			trustConnection(1, nil, false, ""),
			trustConnection(total, labelRun(start.Add(time.Hour*100), 3), false, "timeline-2")),
	)

	got, err := c.ReadIssueTrust(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if len(got.LabelEvents) != total {
		t.Fatalf("read %d label events, want %d", len(got.LabelEvents), total)
	}
	last := got.LabelEvents[total-1]
	if !last.Added || !last.CreatedAt.Equal(start.Add(time.Hour*100+2*time.Minute)) {
		t.Errorf("the last label event read is %+v, not the last page's last", last)
	}
	if len(got.Edits) != 1 {
		t.Errorf("read %d edits, want 1", len(got.Edits))
	}
	sent := sentQueries(t, net)
	if sent[1].Variables["timelineCursor"] != "timeline-1" {
		t.Errorf("the follow-up asked for label events after %v", sent[1].Variables["timelineCursor"])
	}
	if _, ok := sent[1].Variables["editsCursor"]; ok || strings.Contains(sent[1].Query, "editedAt") {
		t.Errorf("the follow-up for label events also paged edits:\n%s", sent[1].Query)
	}
}

// Every page after the first describes the body the first page returned, or
// the read is refused: a body edited between pages is one whose history the
// pages do not describe.
func TestABodyThatChangesBetweenPagesIsRefused(t *testing.T) {
	start := mustTime(t, "2026-01-01T00:00:00Z")
	edited := defaultTrustState
	edited.body = "## Goal\nDo something else."
	moved := defaultTrustState
	moved.lastEditedAt = "2026-03-09T10:00:00Z"
	for name, later := range map[string]struct {
		state    trustIssueState
		editsLen int
		labels   int
	}{
		"a different body":           {state: edited, editsLen: trustPageLength + 1, labels: 1},
		"a later last edit":          {state: moved, editsLen: trustPageLength + 1, labels: 1},
		"a longer edit history":      {state: defaultTrustState, editsLen: trustPageLength + 2, labels: 1},
		"a different label timeline": {state: defaultTrustState, editsLen: trustPageLength + 1, labels: 2},
	} {
		t.Run(name, func(t *testing.T) {
			c, net := readClient(t,
				trustAnswer(t, defaultTrustState,
					trustConnection(trustPageLength+1, editsRun(start, trustPageLength), true, "edits-1"),
					trustConnection(1, labelRun(start, 1), false, "")),
				trustAnswer(t, later.state,
					trustConnection(later.editsLen, editsRun(start, 1), false, "edits-2"),
					trustConnection(later.labels, nil, false, "")),
			)
			got, err := c.ReadIssueTrust(context.Background(), 12)
			net.done()
			if err == nil {
				t.Fatalf("a read across %s succeeded with %d edits", name, len(got.Edits))
			}
		})
	}
}

// A connection whose pages do not add up to the size GitHub reports for it is
// short, and a short history read as a whole one hides an editor.
func TestAHistoryShorterThanItsReportedSizeIsRefused(t *testing.T) {
	start := mustTime(t, "2026-01-01T00:00:00Z")
	c, net := readClient(t, trustAnswer(t, defaultTrustState,
		trustConnection(3, editsRun(start, 2), false, ""),
		trustConnection(1, labelRun(start, 1), false, "")))
	if _, err := c.ReadIssueTrust(context.Background(), 12); err == nil {
		t.Error("an edit history shorter than its reported size was read as whole")
	}
	net.done()

	c, net = readClient(t, trustAnswer(t, defaultTrustState,
		trustConnection(1, editsRun(start, 1), false, ""),
		trustConnection(2, labelRun(start, 1), false, "")))
	if _, err := c.ReadIssueTrust(context.Background(), 12); err == nil {
		t.Error("label events shorter than their reported size were read as whole")
	}
	net.done()
}

// A history that never ends is a cursor loop, refused rather than followed.
func TestAnEditHistoryThatNeverEndsIsRefused(t *testing.T) {
	start := mustTime(t, "2026-01-01T00:00:00Z")
	const maxPagesForTest = 40
	total := trustPageLength * (maxPagesForTest + 1)
	exchanges := []exchange{trustAnswer(t, defaultTrustState,
		trustConnection(total, editsRun(start, trustPageLength), true, "edits"),
		trustConnection(0, []map[string]any{}, false, ""))}
	for range maxPagesForTest - 1 {
		exchanges = append(exchanges, trustAnswer(t, defaultTrustState,
			trustConnection(total, editsRun(start, trustPageLength), true, "edits"),
			trustConnection(0, nil, false, "")))
	}
	c, net := readClient(t, exchanges...)
	if _, err := c.ReadIssueTrust(context.Background(), 12); err == nil {
		t.Error("an edit history that never ended was read as whole")
	}
	net.done()
}

// GraphQL refused by the network is "the history cannot be read here", which
// is neither an empty history nor a refusal of the token.
func TestGraphQLBlockedByTheNetworkIsReportedAsUnavailable(t *testing.T) {
	c, net := readClient(t, blockedByProxy())
	got, err := c.ReadIssueTrust(context.Background(), 12)
	net.done()
	if !errors.Is(err, githubapp.ErrGraphQLUnavailable) {
		t.Fatalf("a GraphQL refusal from the network read as %v with %+v", err, got)
	}
	if errors.Is(err, githubapp.ErrForbidden) || errors.Is(err, githubapp.ErrNotFound) {
		t.Errorf("a network that blocks GraphQL read as a refusal of the token: %v", err)
	}
	sentQueries(t, net)
}

// Every other failure is an error, never an empty history, and never the
// network's refusal of GraphQL.
func TestAGraphQLRefusalIsAnErrorRatherThanAnEmptyHistory(t *testing.T) {
	for name, answer := range map[string]exchange{
		"an errors array in a 200": query(`{"data":null,"errors":[{"type":"FORBIDDEN",` +
			`"message":"Resource not accessible by personal access token"}]}`),
		"an errors array beside partial data": query(`{"data":{"repository":{"issue":null}},"errors":[` +
			`{"type":"INTERNAL","message":"Something went wrong"}]}`),
		"a 502":           {method: http.MethodPost, path: graphqlPath, status: 502, body: `{"message":"Bad gateway"}`},
		"an ordinary 403": {method: http.MethodPost, path: graphqlPath, status: 403, body: `{"message":"Forbidden"}`},
	} {
		t.Run(name, func(t *testing.T) {
			c, net := readClient(t, answer)
			got, err := c.ReadIssueTrust(context.Background(), 12)
			net.done()
			if err == nil {
				t.Fatalf("%s read as %+v", name, got)
			}
			if errors.Is(err, githubapp.ErrGraphQLUnavailable) {
				t.Errorf("%s read as a network that blocks GraphQL: %v", name, err)
			}
		})
	}
}

func TestAnIssueGitHubCannotFindIsNotFound(t *testing.T) {
	for name, answer := range map[string]string{
		"a NOT_FOUND error": `{"data":{"repository":{"issue":null}},"errors":[{"type":"NOT_FOUND",` +
			`"message":"Could not resolve to an Issue with the number of 12."}]}`,
		"a null issue":      `{"data":{"repository":{"issue":null}}}`,
		"a null repository": `{"data":{"repository":null}}`,
	} {
		t.Run(name, func(t *testing.T) {
			c, net := readClient(t, query(answer))
			_, err := c.ReadIssueTrust(context.Background(), 12)
			net.done()
			if !errors.Is(err, githubapp.ErrNotFound) {
				t.Errorf("%s read as %v", name, err)
			}
		})
	}
}

// A label event of a type the query did not ask for is an answer this
// reading does not understand, and is refused rather than dropped.
func TestALabelEventOfAnUnaskedTypeIsRefused(t *testing.T) {
	start := mustTime(t, "2026-01-01T00:00:00Z")
	c, _ := readClient(t, trustAnswer(t, defaultTrustState,
		trustConnection(0, []map[string]any{}, false, ""),
		trustConnection(1, []map[string]any{trustLabelEvent("RenamedTitleEvent", "x", nil, start)}, false, "")))
	if _, err := c.ReadIssueTrust(context.Background(), 12); err == nil {
		t.Error("an unasked timeline item type was read as a label event")
	}
}

func TestAnIssueTrustNumberBelowOneNeverReachesTheNetwork(t *testing.T) {
	c, net := readClient(t)
	for _, n := range []int{0, -1} {
		if _, err := c.ReadIssueTrust(context.Background(), n); err == nil {
			t.Errorf("issue %d was read", n)
		}
	}
	net.done()
}
