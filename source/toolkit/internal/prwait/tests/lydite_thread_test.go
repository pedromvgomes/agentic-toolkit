package tests

import (
	"context"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/prwait"
)

// mutantThread is a thread lydite opened on a line where a mutant survived,
// with nobody else having spoken on it.
func mutantThread(author string) githubapp.ReviewThread {
	return githubapp.ReviewThread{
		Path: "calc.go", Body: "🧬 **Surviving mutant**: `a + b` → `a - b`",
		RootAuthor: author, LastAuthor: author,
	}
}

func TestALyditeMutantThreadOnAGreenHeadIsClean(t *testing.T) {
	reader := &fakeReader{states: []state{{
		pr:       open(headA),
		runs:     []githubapp.CheckRun{run("build", "completed", "success"), run("lydite", "completed", "success")},
		statuses: []githubapp.CommitStatus{status("lydite/test", "success")},
		threads:  []githubapp.ReviewThread{mutantThread("lydite[bot]"), mutantThread("Lydite[Bot]")},
	}}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.Clean || res.Verdict.ExitCode() != prwait.ExitClean || res.Polls != 1 {
		t.Fatalf("verdict %q after %d polls, want clean after 1: %+v", res.Verdict, res.Polls, res)
	}
	if len(res.WaitingThreads) != 0 || res.AnsweredUnresolved != 0 {
		t.Errorf("a lydite mutant thread was judged: waiting %+v, answered %d", res.WaitingThreads, res.AnsweredUnresolved)
	}
}

func TestALyditeMutantThreadNeedsActionThroughLyditesFailingCheckAndNeverAsAWaitingThread(t *testing.T) {
	reader := &fakeReader{states: []state{{
		pr:       open(headA),
		runs:     []githubapp.CheckRun{run("build", "completed", "success"), run("lydite", "completed", "failure")},
		threads:  []githubapp.ReviewThread{mutantThread("lydite[bot]")},
		comments: []githubapp.IssueComment{{ID: 1, Author: "lydite[bot]", Body: redComment}},
	}}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.ActionNeeded || res.Verdict.ExitCode() != prwait.ExitActionNeeded {
		t.Fatalf("verdict %q, want action-needed", res.Verdict)
	}
	if len(res.Failing) != 1 || res.Failing[0].Name != "lydite" {
		t.Errorf("failing = %+v, want lydite's check alone", res.Failing)
	}
	if len(res.WaitingThreads) != 0 || res.AnsweredUnresolved != 0 {
		t.Errorf("a lydite mutant thread was judged: waiting %+v, answered %d", res.WaitingThreads, res.AnsweredUnresolved)
	}
}

func TestALyditeMutantThreadDoesNotEndTheWaitWhileLyditeIsRunning(t *testing.T) {
	reader := &fakeReader{states: []state{
		{
			pr:      open(headA),
			runs:    []githubapp.CheckRun{run("build", "completed", "success"), run("lydite", "in_progress", "")},
			threads: []githubapp.ReviewThread{mutantThread("lydite[bot]")},
		},
		{
			pr:   open(headA),
			runs: []githubapp.CheckRun{run("build", "completed", "success"), run("lydite", "completed", "success")},
		},
	}}
	var reports []prwait.PollReport
	res := prwait.Wait(context.Background(), reader, prwait.Options{
		Number: 7, Poll: time.Minute, Cap: time.Hour, Clock: newClock(),
		Progress: func(r prwait.PollReport) { reports = append(reports, r) },
	})
	if len(reports) == 0 || reports[0].WaitingThreads != 0 || reports[0].Pending != 1 || reports[0].Failing != 0 {
		t.Fatalf("the first poll = %+v, want lydite pending and no waiting thread", reports)
	}
	if res.Verdict != prwait.Clean || res.Polls != 2 {
		t.Errorf("verdict %q after %d polls, want clean after 2", res.Verdict, res.Polls)
	}
}

func TestAThreadCopyingLyditesTextFromAnotherAccountIsWaiting(t *testing.T) {
	for _, author := range []string{"alice", "github-actions[bot]", "lydite", "xlydite[bot]"} {
		reader := &fakeReader{states: []state{{
			pr:      open(headA),
			runs:    []githubapp.CheckRun{run("build", "completed", "success")},
			threads: []githubapp.ReviewThread{mutantThread(author)},
		}}}
		res := wait(t, reader, newClock())
		if res.Verdict != prwait.ActionNeeded || len(res.WaitingThreads) != 1 {
			t.Errorf("%s: verdict %q, waiting %+v; want the thread waiting", author, res.Verdict, res.WaitingThreads)
		}
	}
}

func TestALyditeThreadSomebodyElseRepliedToIsWaiting(t *testing.T) {
	thread := mutantThread("lydite[bot]")
	thread.LastAuthor = "alice"
	reader := &fakeReader{states: []state{{
		pr:      open(headA),
		runs:    []githubapp.CheckRun{run("build", "completed", "success")},
		threads: []githubapp.ReviewThread{thread},
	}}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.ActionNeeded || len(res.WaitingThreads) != 1 || res.WaitingThreads[0].Path != "calc.go" {
		t.Errorf("verdict %q, waiting %+v; want the replied-to thread waiting", res.Verdict, res.WaitingThreads)
	}
}

func TestOnlyTheConfiguredLyditeAppsMutantThreadsAreLeftOut(t *testing.T) {
	green := func(threads ...githubapp.ReviewThread) *fakeReader {
		return &fakeReader{states: []state{{
			pr:      open(headA),
			runs:    []githubapp.CheckRun{run("build", "completed", "success")},
			threads: threads,
		}}}
	}
	if res := waitFor(green(mutantThread("other[bot]")), "other"); res.Verdict != prwait.Clean {
		t.Errorf("other[bot]'s thread with the App set to other: verdict %q, waiting %+v", res.Verdict, res.WaitingThreads)
	}
	if res := waitFor(green(mutantThread("lydite[bot]")), "other"); res.Verdict != prwait.ActionNeeded || len(res.WaitingThreads) != 1 {
		t.Errorf("lydite[bot]'s thread with the App set to other: verdict %q, waiting %+v", res.Verdict, res.WaitingThreads)
	}
}
