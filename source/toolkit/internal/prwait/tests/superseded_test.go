package tests

import (
	"fmt"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/prwait"
)

// workflowRun is a run of the GitHub Actions workflow with id workflow, triggered by
// event, whose check runs sit in the suite with id suite, created minute
// minutes past 11:00.
func workflowRun(id, workflow, suite int64, event string, minute int, status, conclusion string) githubapp.WorkflowRun {
	return githubapp.WorkflowRun{
		ID: id, Name: "CI", WorkflowID: workflow, Path: ".github/workflows/ci.yml", Event: event,
		CheckSuiteID: suite, RunAttempt: 1, Status: status, Conclusion: conclusion,
		HTMLURL:   fmt.Sprint("https://github.com/o/r/actions/runs/", id),
		CreatedAt: time.Date(2026, 1, 1, 11, minute, 0, 0, time.UTC),
	}
}

// inSuite is a GitHub Actions check run in the suite with id suite.
func inSuite(suite int64, name, status, conclusion string) githubapp.CheckRun {
	r := run(name, status, conclusion)
	r.App, r.CheckSuiteID = "github-actions", suite
	return r
}

// Two pull_request events on one head start two runs of one GitHub Actions workflow;
// the workflow's concurrency cancels the older, whose matrix jobs never expanded.
// Its cancelled and failed check runs stay on the head beside the newer run's
// green ones, under names the newer run does not carry.
func TestAnOlderRunOfAWorkflowCancelledByANewerOneIsNotJudged(t *testing.T) {
	reader := &fakeReader{states: []state{{
		pr: open(headA),
		workflows: []githubapp.WorkflowRun{
			workflowRun(44020563390, 9, 200, "pull_request", 5, "completed", "success"),
			workflowRun(44020526981, 9, 100, "pull_request", 4, "completed", "cancelled"),
		},
		runs: []githubapp.CheckRun{
			inSuite(200, "test (unit)", "completed", "success"),
			inSuite(200, "test (integration)", "completed", "success"),
			inSuite(200, "build", "completed", "success"),
			inSuite(100, "test (${{ matrix.shard.name }})", "completed", "cancelled"),
			inSuite(100, "build", "completed", "failure"),
		},
	}}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.Clean {
		t.Fatalf("verdict %q, want clean: failing %+v, pending %+v", res.Verdict, res.Failing, res.Pending)
	}
}

func TestANewerRunThatFailsStillNeedsActionOverAnOlderGreenOne(t *testing.T) {
	reader := &fakeReader{states: []state{{
		pr: open(headA),
		workflows: []githubapp.WorkflowRun{
			workflowRun(1, 9, 100, "pull_request", 4, "completed", "success"),
			workflowRun(2, 9, 200, "pull_request", 5, "completed", "failure"),
		},
		runs: []githubapp.CheckRun{
			inSuite(100, "test", "completed", "success"),
			inSuite(200, "test", "completed", "failure"),
		},
	}}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.ActionNeeded {
		t.Fatalf("verdict %q, want action-needed", res.Verdict)
	}
	if len(res.Failing) != 1 || res.Failing[0].Kind != "check-run" || res.Failing[0].State != "failure" {
		t.Errorf("failing = %+v, want the newer run's failed test alone", res.Failing)
	}
}

// Two runs created in the same second are ordered by run ID.
func TestBetweenRunsCreatedTogetherTheHigherIDIsTheNewest(t *testing.T) {
	reader := &fakeReader{states: []state{{
		pr: open(headA),
		workflows: []githubapp.WorkflowRun{
			workflowRun(8, 9, 800, "pull_request", 5, "completed", "failure"),
			workflowRun(7, 9, 700, "pull_request", 5, "completed", "success"),
		},
		runs: []githubapp.CheckRun{
			inSuite(700, "test", "completed", "success"),
			inSuite(800, "test", "completed", "failure"),
		},
	}}}
	if res := wait(t, reader, newClock()); res.Verdict != prwait.ActionNeeded {
		t.Fatalf("verdict %q, want action-needed from run 8", res.Verdict)
	}
}

// Two runs with the same creation time and the same ID cannot be ordered, so
// the run GitHub listed first is the one judged.
func TestAFullTieBetweenRunsKeepsTheRunGitHubListedFirst(t *testing.T) {
	reader := &fakeReader{states: []state{{
		pr: open(headA),
		workflows: []githubapp.WorkflowRun{
			workflowRun(7, 9, 700, "pull_request", 5, "completed", "success"),
			workflowRun(7, 9, 800, "pull_request", 5, "completed", "success"),
		},
		runs: []githubapp.CheckRun{
			inSuite(700, "test", "completed", "success"),
			inSuite(800, "test", "completed", "failure"),
		},
	}}}
	if res := wait(t, reader, newClock()); res.Verdict != prwait.Clean {
		t.Fatalf("verdict %q, want clean from the run listed first", res.Verdict)
	}
}

// A check run another App created belongs to no GitHub Actions workflow run, so nothing
// supersedes it.
func TestACheckRunInASuiteNoWorkflowRunOwnsIsJudgedAsItIs(t *testing.T) {
	other := run("external-ci", "completed", "failure")
	other.App, other.CheckSuiteID = "circleci", 900
	reader := &fakeReader{states: []state{{
		pr: open(headA),
		workflows: []githubapp.WorkflowRun{
			workflowRun(1, 9, 100, "pull_request", 4, "completed", "cancelled"),
			workflowRun(2, 9, 200, "pull_request", 5, "completed", "success"),
		},
		runs: []githubapp.CheckRun{
			inSuite(100, "build", "completed", "cancelled"),
			inSuite(200, "build", "completed", "success"),
			other,
		},
	}}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.ActionNeeded || len(res.Failing) != 1 || res.Failing[0].Name != "external-ci" {
		t.Fatalf("verdict %q, failing %+v, want the other App's failed run alone", res.Verdict, res.Failing)
	}
}

// A push run and a pull_request run of one GitHub Actions workflow test the head under
// different events, so neither replaces the other.
func TestRunsOfOneWorkflowTriggeredByDifferentEventsAreBothJudged(t *testing.T) {
	reader := &fakeReader{states: []state{{
		pr: open(headA),
		workflows: []githubapp.WorkflowRun{
			workflowRun(1, 9, 100, "push", 4, "completed", "failure"),
			workflowRun(2, 9, 200, "pull_request", 5, "completed", "success"),
		},
		runs: []githubapp.CheckRun{
			inSuite(100, "build", "completed", "failure"),
			inSuite(200, "build", "completed", "success"),
		},
	}}}
	if res := wait(t, reader, newClock()); res.Verdict != prwait.ActionNeeded || len(res.Failing) != 1 {
		t.Fatalf("verdict %q, failing %+v, want the push run's failure", res.Verdict, res.Failing)
	}
}

// The newest run of a GitHub Actions workflow may have created no check run yet. It stands
// in for them, so the older run it supersedes does not leave the workflow
// with nothing judged.
func TestANewestRunWithNoCheckRunsIsJudgedByItsOwnStatus(t *testing.T) {
	queued := workflowRun(2, 9, 200, "pull_request", 5, "queued", "")
	reader := &fakeReader{states: []state{
		{
			pr:        open(headA),
			workflows: []githubapp.WorkflowRun{workflowRun(1, 9, 100, "pull_request", 4, "completed", "success"), queued},
			runs:      []githubapp.CheckRun{inSuite(100, "build", "completed", "success")},
		},
		{
			pr:        open(headA),
			workflows: []githubapp.WorkflowRun{workflowRun(1, 9, 100, "pull_request", 4, "completed", "success"), workflowRun(2, 9, 200, "pull_request", 5, "completed", "startup_failure")},
			runs:      []githubapp.CheckRun{inSuite(100, "build", "completed", "success")},
		},
	}}
	var pending []int
	res := prwait.Wait(t.Context(), reader, prwait.Options{
		Number: 7, Poll: time.Minute, Cap: time.Hour, Clock: newClock(),
		Progress: func(r prwait.PollReport) { pending = append(pending, r.Pending) },
	})
	if res.Verdict != prwait.ActionNeeded || res.Polls != 2 {
		t.Fatalf("verdict %q after %d polls, want action-needed after 2", res.Verdict, res.Polls)
	}
	if len(pending) != 2 || pending[0] != 1 {
		t.Errorf("pending per poll = %v, want the queued run pending on the first", pending)
	}
	want := prwait.Item{Kind: "workflow-run", Name: "CI", State: "startup_failure", URL: queued.HTMLURL}
	if len(res.Failing) != 1 || res.Failing[0] != want {
		t.Errorf("failing = %+v, want [%+v]", res.Failing, want)
	}
}

// A run that starts between the GitHub Actions workflow run read and the check run read is
// missing from the first, so its check runs are judged beside the run it
// supersedes rather than leaving the workflow with nothing judged.
func TestACheckRunOfARunTheWorkflowReadMissedIsJudged(t *testing.T) {
	reader := &fakeReader{states: []state{{
		pr:        open(headA),
		workflows: []githubapp.WorkflowRun{workflowRun(1, 9, 100, "pull_request", 4, "completed", "success")},
		runs: []githubapp.CheckRun{
			inSuite(100, "build", "completed", "success"),
			inSuite(200, "build", "in_progress", ""),
		},
	}}}
	reader.states = append(reader.states, reader.states[0])
	res := prwait.Wait(t.Context(), reader, prwait.Options{Number: 7, Poll: time.Minute, Cap: time.Minute, Clock: newClock()})
	if res.Verdict != prwait.CapReached || len(res.Pending) != 1 {
		t.Fatalf("verdict %q, pending %+v, want the unlisted run's check pending", res.Verdict, res.Pending)
	}
}

// A token without actions: read is refused the GitHub Actions workflow runs. Nothing then
// ties a check run to a newer run of its workflow, so every check run is
// judged as it is and the superseded run's failure needs action.
func TestAForbiddenWorkflowRunReadJudgesEveryCheckRunAsItIs(t *testing.T) {
	reader := &fakeReader{states: []state{{
		pr:           open(headA),
		workflowsErr: fmt.Errorf("%w: GitHub answered 403", githubapp.ErrForbidden),
		runs: []githubapp.CheckRun{
			inSuite(200, "build", "completed", "success"),
			inSuite(100, "build", "completed", "failure"),
		},
	}}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.ActionNeeded || len(res.Failing) != 1 || res.Failing[0].Name != "build" {
		t.Fatalf("verdict %q, failing %+v, want the superseded run's failure", res.Verdict, res.Failing)
	}
	if reader.polls != 1 {
		t.Errorf("%d polls, want the verdict on the first", reader.polls)
	}
}

// A GitHub Actions workflow run read failing any other way fails the poll as every other read
// failure does.
func TestAWorkflowRunReadFailureOtherThanForbiddenFailsThePoll(t *testing.T) {
	reader := &fakeReader{states: []state{{
		pr:           open(headA),
		workflowsErr: errTransient,
		runs:         []githubapp.CheckRun{inSuite(100, "build", "completed", "success")},
	}}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.CouldNotRead {
		t.Fatalf("verdict %q, want could-not-read", res.Verdict)
	}
}
