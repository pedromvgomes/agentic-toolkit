package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/prwait"
)

func wait(t *testing.T, reader *fakeReader, clock *fakeClock) prwait.Result {
	t.Helper()
	return prwait.Wait(context.Background(), reader, prwait.Options{
		Number: 7, Poll: 5 * time.Minute, Cap: 2 * time.Hour, Clock: clock,
	})
}

func TestAHeadWhoseChecksAllPassIsCleanOnTheFirstPoll(t *testing.T) {
	reader := &fakeReader{states: []state{{
		pr:       open(headA),
		runs:     []githubapp.CheckRun{run("build", "completed", "success"), run("docs", "completed", "skipped"), run("lint", "completed", "neutral")},
		statuses: []githubapp.CommitStatus{status("ci/legacy", "success")},
	}}}
	clock := newClock()
	res := wait(t, reader, clock)
	if res.Verdict != prwait.Clean || res.Verdict.ExitCode() != prwait.ExitClean {
		t.Fatalf("verdict %q, want clean: %+v", res.Verdict, res)
	}
	if res.Polls != 1 || len(clock.sleeps) != 0 || res.HeadSHA != headA {
		t.Errorf("polls %d, sleeps %v, head %q", res.Polls, clock.sleeps, res.HeadSHA)
	}
	if res.ReferralPending || res.Lydite != nil {
		t.Errorf("a head with no referral reports one: %+v", res)
	}
}

func TestAPendingReferralIsTheOneStatusACleanHeadMayCarry(t *testing.T) {
	reader := &fakeReader{states: []state{{
		pr:       open(headA),
		runs:     []githubapp.CheckRun{run("build", "completed", "success")},
		statuses: []githubapp.CommitStatus{status("lydite/referral", "pending"), status("lydite/test", "success")},
		comments: []githubapp.IssueComment{{ID: 1, Author: "lydite[bot]", Body: referralComment, UpdatedAt: time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)}},
	}}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.Clean {
		t.Fatalf("verdict %q, want clean: %+v", res.Verdict, res)
	}
	if !res.ReferralPending {
		t.Error("the pending referral is not reported")
	}
	if res.Lydite == nil || res.Lydite.Verdict != "referral" || len(res.Lydite.Rows) == 0 {
		t.Errorf("the referral's detail is missing: %+v", res.Lydite)
	}
}

func TestAPendingStatusOtherThanTheReferralKeepsTheWaitPolling(t *testing.T) {
	reader := &fakeReader{states: []state{
		{pr: open(headA), runs: []githubapp.CheckRun{run("build", "completed", "success")}, statuses: []githubapp.CommitStatus{status("lydite/test", "pending")}},
		{pr: open(headA), runs: []githubapp.CheckRun{run("build", "completed", "success")}, statuses: []githubapp.CommitStatus{status("lydite/test", "success")}},
	}}
	clock := newClock()
	res := wait(t, reader, clock)
	if res.Verdict != prwait.Clean || res.Polls != 2 {
		t.Fatalf("verdict %q after %d polls, want clean after 2", res.Verdict, res.Polls)
	}
	if len(clock.sleeps) != 1 || clock.sleeps[0] != 5*time.Minute {
		t.Errorf("slept %v, want one poll interval", clock.sleeps)
	}
	if res.Elapsed != 5*time.Minute {
		t.Errorf("elapsed %s, want 5m", res.Elapsed)
	}
}

func TestARunningCheckIsPendingAndAFailedOneOnTheNextPollNeedsAction(t *testing.T) {
	reader := &fakeReader{states: []state{
		{pr: open(headA), runs: []githubapp.CheckRun{run("test", "in_progress", "")}},
		{pr: open(headA), runs: []githubapp.CheckRun{run("test", "completed", "failure"), run("build", "completed", "success")}},
	}}
	var reports []prwait.PollReport
	res := prwait.Wait(context.Background(), reader, prwait.Options{
		Number: 7, Poll: time.Minute, Cap: time.Hour, Clock: newClock(),
		Progress: func(r prwait.PollReport) { reports = append(reports, r) },
	})
	if res.Verdict != prwait.ActionNeeded || res.Verdict.ExitCode() != prwait.ExitActionNeeded {
		t.Fatalf("verdict %q, want action-needed", res.Verdict)
	}
	want := prwait.Item{Kind: "check-run", Name: "test", State: "failure", URL: "https://github.com/o/r/runs/test"}
	if len(res.Failing) != 1 || res.Failing[0] != want {
		t.Errorf("failing = %+v, want [%+v]", res.Failing, want)
	}
	if len(reports) != 2 || reports[0].Pending != 1 || reports[1].Failing != 1 {
		t.Errorf("progress = %+v", reports)
	}
}

func TestEveryNonPassingConclusionNeedsAction(t *testing.T) {
	for _, conclusion := range []string{"failure", "cancelled", "timed_out", "action_required", "stale", ""} {
		reader := &fakeReader{states: []state{{pr: open(headA), runs: []githubapp.CheckRun{run("job", "completed", conclusion)}}}}
		if res := wait(t, reader, newClock()); res.Verdict != prwait.ActionNeeded {
			t.Errorf("conclusion %q: verdict %q, want action-needed", conclusion, res.Verdict)
		}
	}
}

func TestAFailedOrErroredStatusNeedsAction(t *testing.T) {
	for _, st := range []string{"failure", "error"} {
		reader := &fakeReader{states: []state{{
			pr:       open(headA),
			runs:     []githubapp.CheckRun{run("build", "completed", "success")},
			statuses: []githubapp.CommitStatus{status("lydite/scan", st)},
		}}}
		res := wait(t, reader, newClock())
		if res.Verdict != prwait.ActionNeeded || len(res.Failing) != 1 || res.Failing[0].Name != "lydite/scan" {
			t.Errorf("status %q: verdict %q, failing %+v", st, res.Verdict, res.Failing)
		}
	}
}

func TestAFailingLyditeStatusReportsTheCommentsRedRows(t *testing.T) {
	reader := &fakeReader{states: []state{{
		pr:       open(headA),
		statuses: []githubapp.CommitStatus{status("lydite/test", "failure")},
		comments: []githubapp.IssueComment{
			{ID: 1, Author: "alice", Body: "<!-- lydite:results -->\n### ✅ All green"},
			{ID: 2, Author: "lydite[bot]", Body: redComment, CreatedAt: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)},
		},
	}}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.ActionNeeded {
		t.Fatalf("verdict %q, want action-needed", res.Verdict)
	}
	if res.Lydite == nil || res.Lydite.Verdict != "red" {
		t.Fatalf("lydite detail = %+v, want the bot's red comment and never the one a person wrote", res.Lydite)
	}
	if res.Lydite.PossiblyStale {
		t.Error("a comment written with no lydite check seen running is marked stale")
	}
}

func TestAnUnresolvedThreadIsWaitingAndAResolvedOneIsNot(t *testing.T) {
	reader := &fakeReader{states: []state{{
		pr:   open(headA),
		runs: []githubapp.CheckRun{run("build", "completed", "success")},
		threads: []githubapp.ReviewThread{
			{Path: "a.go", Resolved: true, Body: "fixed"},
			{Path: "b.go", Outdated: true, Body: "**Finding**: the error is dropped\n\nmore detail"},
		},
	}}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.ActionNeeded {
		t.Fatalf("verdict %q, want action-needed", res.Verdict)
	}
	want := prwait.Thread{Path: "b.go", Outdated: true, Excerpt: "**Finding**: the error is dropped"}
	if len(res.WaitingThreads) != 1 || res.WaitingThreads[0] != want {
		t.Errorf("waiting = %+v, want [%+v]", res.WaitingThreads, want)
	}
}

func TestAThreadIsAnsweredWhenTheTokensAccountSpokeLastAndWaitingOtherwise(t *testing.T) {
	reader := &fakeReader{states: []state{{
		pr:   open(headA),
		runs: []githubapp.CheckRun{run("build", "completed", "success")},
		threads: []githubapp.ReviewThread{
			{Path: "answered.go", Body: "**Finding**: answered", LastAuthor: viewer},
			{Path: "answered-any-case.go", Body: "**Finding**: answered", LastAuthor: "Agent-Bot"},
			{Path: "outdated.go", Outdated: true, Body: "**Finding**: outdated and answered", LastAuthor: viewer},
			{Path: "reviewer.go", Body: "**Finding**: a reviewer spoke last", LastAuthor: "alice"},
			{Path: "deleted.go", Body: "**Finding**: its last author is gone"},
			{Path: "resolved.go", Resolved: true, Body: "fixed", LastAuthor: "alice"},
		},
	}}}
	var reports []prwait.PollReport
	res := prwait.Wait(context.Background(), reader, prwait.Options{
		Number: 7, Poll: time.Minute, Cap: time.Hour, Clock: newClock(),
		Progress: func(r prwait.PollReport) { reports = append(reports, r) },
	})
	if res.Verdict != prwait.ActionNeeded {
		t.Fatalf("verdict %q, want action-needed", res.Verdict)
	}
	want := []prwait.Thread{
		{Path: "reviewer.go", Excerpt: "**Finding**: a reviewer spoke last"},
		{Path: "deleted.go", Excerpt: "**Finding**: its last author is gone"},
	}
	if len(res.WaitingThreads) != len(want) {
		t.Fatalf("waiting = %+v, want %+v", res.WaitingThreads, want)
	}
	for i, w := range want {
		if res.WaitingThreads[i] != w {
			t.Errorf("waiting[%d] = %+v, want %+v", i, res.WaitingThreads[i], w)
		}
	}
	if res.AnsweredUnresolved != 3 {
		t.Errorf("answered unresolved = %d, want 3", res.AnsweredUnresolved)
	}
	if res.Viewer != viewer {
		t.Errorf("viewer = %q, want %q", res.Viewer, viewer)
	}
	if len(reports) != 1 || reports[0].WaitingThreads != 2 || reports[0].AnsweredThreads != 3 {
		t.Errorf("progress = %+v", reports)
	}
}

func TestAHeadWhoseOnlyOpenThreadsAreAnsweredIsClean(t *testing.T) {
	reader := &fakeReader{states: []state{{
		pr:   open(headA),
		runs: []githubapp.CheckRun{run("build", "completed", "success")},
		threads: []githubapp.ReviewThread{
			{Path: "a.go", Body: "**Finding**: answered", LastAuthor: viewer},
			{Path: "b.go", Outdated: true, Body: "**Finding**: outdated and answered", LastAuthor: viewer},
		},
	}}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.Clean || res.Polls != 1 {
		t.Fatalf("verdict %q after %d polls, want clean after 1", res.Verdict, res.Polls)
	}
	if len(res.WaitingThreads) != 0 || res.AnsweredUnresolved != 2 {
		t.Errorf("waiting %+v, answered %d; want none waiting and 2 answered", res.WaitingThreads, res.AnsweredUnresolved)
	}
}

func TestTheViewerIsReadOnceForTheWholeWait(t *testing.T) {
	reader := &fakeReader{states: []state{
		{pr: open(headA), runs: []githubapp.CheckRun{run("build", "queued", "")}},
		{pr: open(headA), runs: []githubapp.CheckRun{run("build", "in_progress", "")}},
		{pr: open(headA), runs: []githubapp.CheckRun{run("build", "completed", "success")}},
	}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.Clean || res.Polls != 3 {
		t.Fatalf("verdict %q after %d polls, want clean after 3", res.Verdict, res.Polls)
	}
	if reader.viewerCalls != 1 {
		t.Errorf("the viewer was read %d times, want once", reader.viewerCalls)
	}
}

func TestAViewerReadFailureIsAFailedPollThatIsRetried(t *testing.T) {
	reader := &fakeReader{
		states:     []state{{pr: open(headA), runs: []githubapp.CheckRun{run("build", "completed", "success")}}},
		viewerErrs: []error{errTransient, errTransient},
	}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.Clean || res.Polls != 3 || res.Err != nil {
		t.Errorf("verdict %q after %d polls, err %v; want clean after 3", res.Verdict, res.Polls, res.Err)
	}
	if len(reader.shas) != 1 {
		t.Errorf("the head's checks were read on %d polls, want only the one that knew the viewer", len(reader.shas))
	}
}

func TestThreeViewerReadFailuresInARowCouldNotRead(t *testing.T) {
	reader := &fakeReader{
		states:     []state{{pr: open(headA), runs: []githubapp.CheckRun{run("build", "completed", "success")}}},
		viewerErrs: []error{errTransient, errTransient, errTransient},
	}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.CouldNotRead || res.Polls != prwait.MaxConsecutiveReadFailures || !errors.Is(res.Err, errTransient) {
		t.Errorf("verdict %q after %d polls, err %v; want could-not-read after 3", res.Verdict, res.Polls, res.Err)
	}
}

func TestAHeadStillWithNoChecksOnTheSecondPollHasNoChecks(t *testing.T) {
	reader := &fakeReader{states: []state{{pr: open(headA)}}}
	var reports []prwait.PollReport
	res := prwait.Wait(context.Background(), reader, prwait.Options{
		Number: 7, Poll: time.Minute, Cap: time.Hour, Clock: newClock(),
		Progress: func(r prwait.PollReport) { reports = append(reports, r) },
	})
	if res.Verdict != prwait.NoChecks || res.Verdict.ExitCode() != prwait.ExitNoChecks {
		t.Fatalf("verdict %q, want no-checks", res.Verdict)
	}
	if res.Polls != 2 || len(reports) != 2 {
		t.Errorf("polls %d, progress %+v; want the verdict on the second poll, the first one pending", res.Polls, reports)
	}
	if len(res.Pending) != 1 || res.Pending[0].Kind != "none" {
		t.Errorf("pending = %+v, want the no-checks placeholder", res.Pending)
	}
}

func TestAReadFailureBetweenTwoEmptyPollsStillEndsAsNoChecks(t *testing.T) {
	reader := &fakeReader{states: []state{
		{pr: open(headA)},
		{pr: open(headA), err: errTransient},
		{pr: open(headA)},
	}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.NoChecks || res.Polls != 3 {
		t.Errorf("verdict %q after %d polls, want no-checks after 3", res.Verdict, res.Polls)
	}
}

func TestAWaitingThreadOnAHeadWithNoChecksNeedsAction(t *testing.T) {
	reader := &fakeReader{states: []state{{
		pr:      open(headA),
		threads: []githubapp.ReviewThread{{Path: "a.go", Body: "**Finding**: x", LastAuthor: "alice"}},
	}}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.ActionNeeded || res.Polls != 1 {
		t.Errorf("verdict %q after %d polls, want action-needed after 1", res.Verdict, res.Polls)
	}
}

func TestAHeadWithNoChecksRegisteredOnTheFirstPollIsPending(t *testing.T) {
	reader := &fakeReader{states: []state{
		{pr: open(headA)},
		{pr: open(headA), runs: []githubapp.CheckRun{run("build", "completed", "success")}},
	}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.Clean || res.Polls != 2 {
		t.Errorf("verdict %q after %d polls, want clean after 2", res.Verdict, res.Polls)
	}
}

func TestAMovedHeadEndsTheWaitWithoutReadingItsChecks(t *testing.T) {
	reader := &fakeReader{states: []state{
		{pr: open(headA), runs: []githubapp.CheckRun{run("build", "queued", "")}},
		{pr: open(headB), runs: []githubapp.CheckRun{run("build", "completed", "success")}},
	}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.HeadMoved || res.Verdict.ExitCode() != prwait.ExitHeadMoved {
		t.Fatalf("verdict %q, want head-moved", res.Verdict)
	}
	if res.HeadSHA != headA || res.NewHeadSHA != headB {
		t.Errorf("head %q → %q", res.HeadSHA, res.NewHeadSHA)
	}
	for _, sha := range reader.shas {
		if sha != headA {
			t.Errorf("read checks on %s, a head the wait was not watching", sha)
		}
	}
}

func TestAClosedPullRequestEndsTheWait(t *testing.T) {
	closed := open(headA)
	closed.State = "closed"
	reader := &fakeReader{states: []state{
		{pr: open(headA), runs: []githubapp.CheckRun{run("build", "queued", "")}},
		{pr: closed},
	}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.Closed || res.Verdict.ExitCode() != prwait.ExitClosed || res.State != "closed" {
		t.Errorf("verdict %q, state %q, want closed", res.Verdict, res.State)
	}
}

func TestThePollThatFindsTheHeadMovedIsReported(t *testing.T) {
	reader := &fakeReader{states: []state{
		{pr: open(headA), runs: []githubapp.CheckRun{run("build", "queued", "")}},
		{pr: open(headB)},
	}}
	var reports []prwait.PollReport
	res := prwait.Wait(context.Background(), reader, prwait.Options{
		Number: 7, Poll: time.Minute, Cap: time.Hour, Clock: newClock(),
		Progress: func(r prwait.PollReport) { reports = append(reports, r) },
	})
	if res.Verdict != prwait.HeadMoved {
		t.Fatalf("verdict %q, want head-moved", res.Verdict)
	}
	if len(reports) != 2 {
		t.Fatalf("progress = %+v, want one report per poll", reports)
	}
	if last := reports[1]; last.Poll != 2 || last.Ended != prwait.HeadMoved || last.NewHeadSHA != headB {
		t.Errorf("final report = %+v, want poll 2 ending head-moved to %s", last, headB)
	}
	if reports[0].Ended != "" {
		t.Errorf("a judged poll reports an ending: %+v", reports[0])
	}
}

func TestThePollThatFindsThePullRequestClosedIsReported(t *testing.T) {
	closed := open(headA)
	closed.State = "closed"
	reader := &fakeReader{states: []state{
		{pr: open(headA), runs: []githubapp.CheckRun{run("build", "queued", "")}},
		{pr: closed},
	}}
	var reports []prwait.PollReport
	res := prwait.Wait(context.Background(), reader, prwait.Options{
		Number: 7, Poll: time.Minute, Cap: time.Hour, Clock: newClock(),
		Progress: func(r prwait.PollReport) { reports = append(reports, r) },
	})
	if res.Verdict != prwait.Closed {
		t.Fatalf("verdict %q, want closed", res.Verdict)
	}
	if len(reports) != 2 {
		t.Fatalf("progress = %+v, want one report per poll", reports)
	}
	if last := reports[1]; last.Poll != 2 || last.Ended != prwait.Closed || last.State != "closed" {
		t.Errorf("final report = %+v, want poll 2 ending closed", last)
	}
}

func TestTheCapEndsAStillPendingWaitAfterAFinalPollAtTheCap(t *testing.T) {
	reader := &fakeReader{states: []state{{pr: open(headA), runs: []githubapp.CheckRun{run("slow", "in_progress", "")}}}}
	clock := newClock()
	res := prwait.Wait(context.Background(), reader, prwait.Options{
		Number: 7, Poll: 5 * time.Minute, Cap: 12 * time.Minute, Clock: clock,
	})
	if res.Verdict != prwait.CapReached || res.Verdict.ExitCode() != prwait.ExitCapReached {
		t.Fatalf("verdict %q, want cap-reached", res.Verdict)
	}
	want := []time.Duration{5 * time.Minute, 5 * time.Minute, 2 * time.Minute}
	if len(clock.sleeps) != len(want) || clock.sleeps[2] != want[2] {
		t.Errorf("sleeps = %v, want %v", clock.sleeps, want)
	}
	if res.Polls != 4 || res.Elapsed != 12*time.Minute {
		t.Errorf("polls %d, elapsed %s; want 4 and 12m", res.Polls, res.Elapsed)
	}
	if len(res.Pending) != 1 || res.Pending[0].Name != "slow" {
		t.Errorf("pending = %+v, want the slow run", res.Pending)
	}
}

func TestAReadFailureIsRetriedAndNotAVerdict(t *testing.T) {
	reader := &fakeReader{states: []state{
		{pr: open(headA), err: errTransient},
		{pr: open(headA), err: errTransient},
		{pr: open(headA), runs: []githubapp.CheckRun{run("build", "completed", "success")}},
	}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.Clean || res.Polls != 3 || res.Err != nil {
		t.Errorf("verdict %q after %d polls, err %v; want clean after 3", res.Verdict, res.Polls, res.Err)
	}
}

func TestThreeConsecutiveReadFailuresCouldNotRead(t *testing.T) {
	reader := &fakeReader{states: []state{
		{pr: open(headA), err: errTransient},
		{pr: open(headA), runs: []githubapp.CheckRun{run("build", "queued", "")}},
		{pr: open(headA), err: errTransient},
	}}
	var reports []prwait.PollReport
	res := prwait.Wait(context.Background(), reader, prwait.Options{
		Number: 7, Poll: time.Minute, Cap: time.Hour, Clock: newClock(),
		Progress: func(r prwait.PollReport) { reports = append(reports, r) },
	})
	if res.Verdict != prwait.CouldNotRead || res.Verdict.ExitCode() != prwait.ExitCouldNotRead {
		t.Fatalf("verdict %q, want could-not-read", res.Verdict)
	}
	if res.Polls != 5 || !errors.Is(res.Err, errTransient) {
		t.Errorf("polls %d, err %v; want 5 polls, the failure after a good one not counting the one before it", res.Polls, res.Err)
	}
	if reports[0].ReadErr == nil || reports[1].ReadErr != nil {
		t.Errorf("progress = %+v", reports)
	}
}

func TestACommentWrittenBeforeLyditeWasSeenRunningIsPossiblyStale(t *testing.T) {
	written := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)
	reader := &fakeReader{states: []state{
		{pr: open(headA), runs: []githubapp.CheckRun{run("lydite / test", "in_progress", "")}},
		{
			pr:       open(headA),
			runs:     []githubapp.CheckRun{run("lydite / test", "completed", "failure")},
			comments: []githubapp.IssueComment{{ID: 1, Author: "lydite[bot]", Body: redComment, UpdatedAt: written}},
		},
	}}
	res := wait(t, reader, newClock())
	if res.Verdict != prwait.ActionNeeded || res.Lydite == nil || !res.Lydite.PossiblyStale {
		t.Errorf("verdict %q, lydite %+v; want the comment marked possibly stale", res.Verdict, res.Lydite)
	}
}

func TestEachVerdictHasItsOwnExitCode(t *testing.T) {
	seen := map[int]prwait.Verdict{}
	for _, v := range []prwait.Verdict{prwait.Clean, prwait.ActionNeeded, prwait.CapReached, prwait.HeadMoved, prwait.Closed, prwait.CouldNotRead, prwait.NoChecks} {
		code := v.ExitCode()
		if other, dup := seen[code]; dup {
			t.Errorf("%q and %q share exit code %d", v, other, code)
		}
		if code == 1 {
			t.Errorf("%q exits 1, the code of a command that never started", v)
		}
		seen[code] = v
	}
}
