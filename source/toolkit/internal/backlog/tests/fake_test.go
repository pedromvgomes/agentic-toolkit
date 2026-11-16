package tests

import (
	"context"
	"errors"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/backlog"
	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

// The token client is what the command layer hands this package, so it has
// to satisfy Reader.
var _ backlog.Reader = (*githubapp.ReadClient)(nil)

// fakeReader answers every read from fields, and records what was read.
type fakeReader struct {
	issue    githubapp.Issue
	issueErr error

	trust    githubapp.IssueTrust
	trustErr error

	perms   map[string]githubapp.Permission
	permErr map[string]error

	labelled    map[string][]githubapp.Issue
	labelledErr error

	predecessors map[int][]githubapp.Issue
	parents      map[int]githubapp.Issue
	parentErr    error

	permReads     []string
	trustReads    int
	labelledReads []string
	issueReads    []int
	parentReads   []int
}

func (f *fakeReader) ReadIssue(_ context.Context, number int) (githubapp.Issue, error) {
	f.issueReads = append(f.issueReads, number)
	if f.issueErr != nil {
		return githubapp.Issue{}, f.issueErr
	}
	out := f.issue
	out.Number = number
	return out, nil
}

func (f *fakeReader) ReadPermission(_ context.Context, login string) (githubapp.Permission, error) {
	f.permReads = append(f.permReads, login)
	if err := f.permErr[login]; err != nil {
		return githubapp.Permission{}, err
	}
	if perm, ok := f.perms[login]; ok {
		return perm, nil
	}
	return githubapp.Permission{Permission: "none", RoleName: "none"}, nil
}

func (f *fakeReader) ReadIssueTrust(_ context.Context, number int) (githubapp.IssueTrust, error) {
	f.trustReads++
	if f.trustErr != nil {
		return githubapp.IssueTrust{}, f.trustErr
	}
	out := f.trust
	out.Number = number
	return out, nil
}

func (f *fakeReader) ReadLabelledIssues(_ context.Context, label string) ([]githubapp.Issue, error) {
	f.labelledReads = append(f.labelledReads, label)
	if f.labelledErr != nil {
		return nil, f.labelledErr
	}
	return f.labelled[label], nil
}

func (f *fakeReader) ReadPredecessors(_ context.Context, number int) ([]githubapp.Issue, error) {
	return f.predecessors[number], nil
}

func (f *fakeReader) ReadParentOfVisibleIssue(_ context.Context, number int) (githubapp.Issue, bool, error) {
	f.parentReads = append(f.parentReads, number)
	if f.parentErr != nil {
		return githubapp.Issue{}, false, f.parentErr
	}
	parent, ok := f.parents[number]
	return parent, ok, nil
}

// errRead stands for any read GitHub did not answer.
var errRead = errors.New("connection reset")

var (
	created = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	edited  = created.Add(time.Hour)
	queued  = created.Add(2 * time.Hour)
)

func user(login string) *githubapp.Actor { return &githubapp.Actor{Login: login, Type: "User"} }

func bot(slug string) *githubapp.Actor { return &githubapp.Actor{Login: slug, Type: "Bot"} }

var (
	writePerm    = githubapp.Permission{Permission: "write", RoleName: "write"}
	maintainPerm = githubapp.Permission{Permission: "write", RoleName: "maintain"}
	adminPerm    = githubapp.Permission{Permission: "admin", RoleName: "admin"}
	triagePerm   = githubapp.Permission{Permission: "read", RoleName: "triage"}
)

// trustedBody carries the bytes a hash is most easily wrong about: a CRLF,
// trailing whitespace and a multibyte character.
const trustedBody = "## Goal\r\nShip it — carefully.  \n"

// trustedItem is an open, queued issue that alice wrote, bob edited before
// alice queued it, with both holding write access.
func trustedItem() *fakeReader {
	return &fakeReader{
		issue: githubapp.Issue{Title: "Do the work", State: "open", Author: "alice",
			Labels: []string{"enhancement", backlog.LabelQueued}},
		trust: githubapp.IssueTrust{
			Body:   trustedBody,
			Author: user("alice"),
			Edits: []githubapp.BodyEdit{
				{Editor: user("alice"), EditedAt: created},
				{Editor: user("bob"), EditedAt: edited},
			},
			LabelEvents: []githubapp.LabelEvent{
				{Label: "enhancement", Added: true, Actor: user("carol"), CreatedAt: created},
				{Label: backlog.LabelQueued, Added: true, Actor: user("alice"), CreatedAt: queued},
			},
		},
		perms: map[string]githubapp.Permission{"alice": adminPerm, "bob": maintainPerm},
	}
}
