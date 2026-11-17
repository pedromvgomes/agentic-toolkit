package tests

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

const (
	headA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	headB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// state is what GitHub answers on one poll.
type state struct {
	pr        githubapp.PullRequest
	workflows []githubapp.WorkflowRun
	runs      []githubapp.CheckRun
	statuses  []githubapp.CommitStatus
	comments  []githubapp.IssueComment
	threads   []githubapp.ReviewThread
	// err fails the poll's check run read.
	err error
	// workflowsErr fails the poll's workflow run read.
	workflowsErr error
}

// fakeReader answers poll n with states[n-1], and the last state for every
// poll after the list ends.
type fakeReader struct {
	states []state
	polls  int
	// shas are the commits the check reads were asked about.
	shas []string
	// viewerErrs fail the viewer reads in turn; once they run out the viewer
	// read answers viewer.
	viewerErrs  []error
	viewerCalls int
}

// viewer is the login every fake token belongs to.
const viewer = "agent-bot"

func (f *fakeReader) current() state {
	return f.states[min(f.polls, len(f.states))-1]
}

func (f *fakeReader) ReadPullRequest(context.Context, int) (githubapp.PullRequest, error) {
	f.polls++
	return f.current().pr, nil
}

func (f *fakeReader) ReadWorkflowRuns(context.Context, string) ([]githubapp.WorkflowRun, error) {
	s := f.current()
	return s.workflows, s.workflowsErr
}

func (f *fakeReader) ReadCheckRuns(_ context.Context, sha string) ([]githubapp.CheckRun, error) {
	f.shas = append(f.shas, sha)
	s := f.current()
	return s.runs, s.err
}

func (f *fakeReader) ReadCommitStatuses(context.Context, string) (githubapp.CombinedStatus, error) {
	return githubapp.CombinedStatus{State: "success", Statuses: f.current().statuses}, nil
}

func (f *fakeReader) ReadIssueComments(context.Context, int) ([]githubapp.IssueComment, error) {
	return f.current().comments, nil
}

func (f *fakeReader) ReadReviewThreads(context.Context, int) ([]githubapp.ReviewThread, error) {
	return f.current().threads, nil
}

func (f *fakeReader) ReadViewer(context.Context) (string, error) {
	f.viewerCalls++
	if f.viewerCalls <= len(f.viewerErrs) {
		return "", f.viewerErrs[f.viewerCalls-1]
	}
	return viewer, nil
}

// fakeClock advances only when slept on.
type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
}

func newClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return nil
}

func open(head string) githubapp.PullRequest {
	return githubapp.PullRequest{Number: 7, State: "open", HeadSHA: head, BaseSHA: strings.Repeat("c", 40)}
}

func run(name, status, conclusion string) githubapp.CheckRun {
	return githubapp.CheckRun{Name: name, Status: status, Conclusion: conclusion, HTMLURL: "https://github.com/o/r/runs/" + name}
}

func status(name, st string) githubapp.CommitStatus {
	return githubapp.CommitStatus{Context: name, State: st, TargetURL: "https://ci.example/" + name}
}

var errTransient = errors.New("GitHub answered 502")
