package backlog

import (
	"context"
	"fmt"
	"slices"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

// Item is one backlog item as List reads it. It says what the item's labels
// and links are, and nothing about whether it is trusted: that is Verify's,
// and List never runs it.
type Item struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	// States are the state labels the item carries, in the canonical order.
	// An item normally carries one; every one it carries is reported, so an
	// item is never shown in a state it does not claim.
	States []string `json:"states"`
	// Stub reports that the item carries agtk:stub.
	Stub bool `json:"stub"`
	// Predecessors are the numbers of the issues the item is blocked by, in
	// ascending order.
	Predecessors []int `json:"predecessors"`
	// PredecessorsDone reports that every predecessor is closed as
	// completed. An item with no predecessors has them all done.
	PredecessorsDone bool `json:"predecessors_done"`
	// Feature is the issue the item is a sub-issue of, and nil when it is
	// none's.
	Feature *Feature `json:"feature"`
}

// Feature is the issue a backlog item is a sub-issue of.
type Feature struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

// List reads the open backlog items: every open issue carrying a canonical
// label, in ascending number order. Pull requests carrying one are skipped.
//
// GitHub's REST API filters by exact label names only, so each canonical
// label is read on its own and an issue found under several is listed once.
// The whole list or an error: a read that fails fails the list, since an
// item left out reads the same as an item that does not exist. repo names
// the repository in errors; reader is already bound to it.
func List(ctx context.Context, reader Reader, repo string) ([]Item, error) {
	found := map[int]githubapp.Issue{}
	for _, label := range labels {
		issues, err := reader.ReadLabelledIssues(ctx, label.Name)
		if err != nil {
			return nil, fmt.Errorf("reading the issues labelled %s on %s: %w", label.Name, repo, err)
		}
		for _, issue := range issues {
			if issue.PullRequest {
				continue
			}
			if _, seen := found[issue.Number]; !seen {
				found[issue.Number] = issue
			}
		}
	}

	numbers := make([]int, 0, len(found))
	for number := range found {
		numbers = append(numbers, number)
	}
	slices.Sort(numbers)

	items := make([]Item, 0, len(numbers))
	for _, number := range numbers {
		item, err := readItem(ctx, reader, repo, found[number])
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// readItem completes one item with its predecessors and its Feature.
func readItem(ctx context.Context, reader Reader, repo string, issue githubapp.Issue) (Item, error) {
	item := Item{
		Number:           issue.Number,
		Title:            issue.Title,
		States:           []string{},
		Stub:             slices.Contains(issue.Labels, LabelStub),
		Predecessors:     []int{},
		PredecessorsDone: true,
	}
	for _, state := range stateLabels {
		if slices.Contains(issue.Labels, state) {
			item.States = append(item.States, state)
		}
	}

	predecessors, err := reader.ReadPredecessors(ctx, issue.Number)
	if err != nil {
		return Item{}, fmt.Errorf("reading the predecessors of %s#%d: %w", repo, issue.Number, err)
	}
	for _, predecessor := range predecessors {
		item.Predecessors = append(item.Predecessors, predecessor.Number)
		if !done(predecessor) {
			item.PredecessorsDone = false
		}
	}
	slices.Sort(item.Predecessors)

	// The label scan returned issue, so it is visible to the reader and a
	// parent read needs no second read of it.
	parent, hasParent, err := reader.ReadParentOfVisibleIssue(ctx, issue.Number)
	if err != nil {
		return Item{}, fmt.Errorf("reading the Feature of %s#%d: %w", repo, issue.Number, err)
	}
	if hasParent {
		item.Feature = &Feature{Number: parent.Number, Title: parent.Title}
	}
	return item, nil
}

// done reports whether a predecessor is done: closed as completed. An issue
// closed as not planned did not deliver what its dependents wait on.
func done(issue githubapp.Issue) bool {
	return issue.State == "closed" && issue.StateReason == "completed"
}
