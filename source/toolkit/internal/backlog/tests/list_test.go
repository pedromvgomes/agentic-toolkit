package tests

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/backlog"
	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

func issue(number int, title string, labels ...string) githubapp.Issue {
	return githubapp.Issue{Number: number, Title: title, State: "open", Labels: labels}
}

func list(t *testing.T, f *fakeReader) []backlog.Item {
	t.Helper()
	items, err := backlog.List(context.Background(), f, repo)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return items
}

func TestListReadsEveryCanonicalLabelAndListsAnItemOnceInNumberOrder(t *testing.T) {
	f := &fakeReader{labelled: map[string][]githubapp.Issue{
		backlog.LabelStub:    {issue(9, "later", backlog.LabelStub, backlog.LabelBlocked)},
		backlog.LabelBlocked: {issue(9, "later", backlog.LabelStub, backlog.LabelBlocked)},
		backlog.LabelQueued:  {issue(4, "first", backlog.LabelQueued)},
	}}
	items := list(t, f)

	var want []string
	for _, label := range backlog.Labels() {
		want = append(want, label.Name)
	}
	if !slices.Equal(f.labelledReads, want) {
		t.Errorf("labels read = %v, want every canonical label %v", f.labelledReads, want)
	}
	if len(items) != 2 || items[0].Number != 4 || items[1].Number != 9 {
		t.Fatalf("items = %+v, want #4 then #9, each once", items)
	}
	if items[0].Title != "first" || !slices.Equal(items[0].States, []string{backlog.LabelQueued}) || items[0].Stub {
		t.Errorf("#4 = %+v", items[0])
	}
	if !items[1].Stub || !slices.Equal(items[1].States, []string{backlog.LabelBlocked}) {
		t.Errorf("#9 = %+v, want a blocked stub", items[1])
	}
}

func TestListSkipsPullRequests(t *testing.T) {
	pr := issue(5, "a pull request", backlog.LabelQueued)
	pr.PullRequest = true
	f := &fakeReader{labelled: map[string][]githubapp.Issue{backlog.LabelQueued: {pr, issue(6, "an issue", backlog.LabelQueued)}}}
	items := list(t, f)
	if len(items) != 1 || items[0].Number != 6 {
		t.Errorf("items = %+v, want only #6", items)
	}
}

func TestListReportsEveryStateAnItemCarries(t *testing.T) {
	both := issue(3, "contradictory", backlog.LabelRunning, backlog.LabelQueued, "bug")
	f := &fakeReader{labelled: map[string][]githubapp.Issue{backlog.LabelQueued: {both}, backlog.LabelRunning: {both}}}
	items := list(t, f)
	if want := []string{backlog.LabelQueued, backlog.LabelRunning}; !slices.Equal(items[0].States, want) {
		t.Errorf("states = %v, want %v", items[0].States, want)
	}
}

func TestAPredecessorIsDoneOnlyWhenClosedAsCompleted(t *testing.T) {
	completed := githubapp.Issue{Number: 1, State: "closed", StateReason: "completed"}
	for name, other := range map[string]githubapp.Issue{
		"open":                  {Number: 2, State: "open"},
		"closed as not planned": {Number: 2, State: "closed", StateReason: "not_planned"},
		"closed with no reason": {Number: 2, State: "closed"},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeReader{
				labelled:     map[string][]githubapp.Issue{backlog.LabelBlocked: {issue(8, "waits", backlog.LabelBlocked)}},
				predecessors: map[int][]githubapp.Issue{8: {other, completed}},
			}
			item := list(t, f)[0]
			if item.PredecessorsDone {
				t.Error("an item waiting on an unfinished predecessor reads as unblocked")
			}
			if !slices.Equal(item.Predecessors, []int{1, 2}) {
				t.Errorf("predecessors = %v, want [1 2]", item.Predecessors)
			}
		})
	}

	f := &fakeReader{
		labelled:     map[string][]githubapp.Issue{backlog.LabelBlocked: {issue(8, "waits", backlog.LabelBlocked)}},
		predecessors: map[int][]githubapp.Issue{8: {completed}},
	}
	if !list(t, f)[0].PredecessorsDone {
		t.Error("an item whose predecessors all completed reads as blocked")
	}
}

func TestAnItemWithNoPredecessorsHasThemAllDone(t *testing.T) {
	f := &fakeReader{labelled: map[string][]githubapp.Issue{backlog.LabelQueued: {issue(2, "free", backlog.LabelQueued)}}}
	if item := list(t, f)[0]; !item.PredecessorsDone || item.Predecessors == nil {
		t.Errorf("item = %+v, want done with an empty, non-nil list", item)
	}
}

func TestAnItemsFeatureIsTheIssueItIsASubIssueOf(t *testing.T) {
	f := &fakeReader{
		labelled: map[string][]githubapp.Issue{backlog.LabelQueued: {issue(2, "child", backlog.LabelQueued), issue(3, "orphan", backlog.LabelQueued)}},
		parents:  map[int]githubapp.Issue{2: {Number: 1, Title: "The feature"}},
	}
	items := list(t, f)
	if got := items[0].Feature; got == nil || *got != (backlog.Feature{Number: 1, Title: "The feature"}) {
		t.Errorf("#2 feature = %+v, want #1", got)
	}
	if items[1].Feature != nil {
		t.Errorf("#3 feature = %+v, want none", items[1].Feature)
	}
}

func TestAnEmptyBacklogAndEmptyFieldsEncodeAsArrays(t *testing.T) {
	items := list(t, &fakeReader{})
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "[]" {
		t.Errorf("empty backlog encodes as %s, want []", raw)
	}

	items = list(t, &fakeReader{labelled: map[string][]githubapp.Issue{backlog.LabelStub: {issue(2, "stub", backlog.LabelStub)}}})
	raw, err = json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"states", "predecessors"} {
		if _, ok := decoded[field].([]any); !ok {
			t.Errorf("%s encodes as %v, want an array", field, decoded[field])
		}
	}
}

func TestAFailedReadFailsTheWholeList(t *testing.T) {
	f := &fakeReader{labelledErr: errRead}
	if items, err := backlog.List(context.Background(), f, repo); !errors.Is(err, errRead) || items != nil {
		t.Errorf("List = %v, %v, want no items and the read failure", items, err)
	}

	f = &fakeReader{
		labelled:  map[string][]githubapp.Issue{backlog.LabelQueued: {issue(2, "child", backlog.LabelQueued)}},
		parentErr: errRead,
	}
	if items, err := backlog.List(context.Background(), f, repo); !errors.Is(err, errRead) || items != nil {
		t.Errorf("List = %v, %v, want no items and the read failure", items, err)
	}
}

// Listing says what labels an item carries; whether its body may run is
// Verify's alone, so List reads no history and no permission.
func TestListNeverVerifies(t *testing.T) {
	f := &fakeReader{labelled: map[string][]githubapp.Issue{backlog.LabelQueued: {issue(2, "queued", backlog.LabelQueued)}}}
	list(t, f)
	if f.trustReads != 0 || len(f.permReads) != 0 {
		t.Errorf("List read %d histories and %d permissions", f.trustReads, len(f.permReads))
	}
}
