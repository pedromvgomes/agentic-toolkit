// Package prwait watches one pull request until a poll reaches a verdict:
// clean, something needing action, the head moving, the pull request closing,
// no checks registered on the head, the cap running out, or GitHub not
// answering.
//
// It reads and decides, and nothing else. Every read goes through Reader,
// which a token's githubapp.ReadClient satisfies; nothing here writes to
// GitHub, and time passes only through Clock, so a test runs the whole wait
// without sleeping.
//
// # What one poll reads
//
// The pull request (its head commit and state), the check runs and the
// combined commit status of that head, the conversation's comments, and the
// review threads. The login of the account the token belongs to is read
// once, on the first poll that gets as far as the head's checks, and kept
// for the rest of the wait.
//
// # What a poll decides
//
//   - Closed: the pull request is not open. Decided before anything else.
//   - Head moved: the head differs from the one the first poll saw. The
//     caller runs the wait again for the new head, which is what restarts the
//     cap on every push.
//   - Action needed: a check run completed with any conclusion other than
//     success, neutral or skipped; a commit status is failure or error, or a
//     state GitHub does not document; or a review thread is waiting.
//   - No checks: the head has no check runs and no statuses on a second
//     judged poll. One empty poll is pending, because CI registers its checks
//     a moment after a push; a head still bare a poll interval later has no
//     CI that is going to report on it, and waiting out the cap on it would
//     report nothing more.
//   - Pending: a check run has not completed, a commit status other than
//     lydite/referral is pending, or the head has no check runs and no
//     statuses on the first judged poll.
//   - Clean: none of the above. lydite/referral is the one status allowed to
//     stay pending: it waits for a human to clear a referral, not for
//     anything still running, and a clean verdict reports it.
//
// # Review threads
//
// A resolved thread is done. An unresolved thread is answered when its latest
// comment was written by the account the token belongs to, and waiting
// otherwise: somebody else spoke last, so the next word is the token owner's.
// Outdated threads follow the same rule, since GitHub's own
// conversation-resolution rule counts them too. A thread whose latest author
// is a deleted account is waiting: nobody can say it was answered.
//
// Only a person resolves a thread, so an answered thread stays unresolved
// until one does. It is reported as a count beside the verdict and never
// holds the wait: counting it as waiting would make every pull request with
// an answered conversation need action on every poll.
//
// # The lydite comment
//
// lydite reports per head through the head's check runs and commit statuses,
// and those are what decide every verdict. Its `<!-- lydite:results -->`
// comment is read only as detail: it names neither the head it describes nor
// when that head was pushed, so it cannot say whether it is current. See
// ParseLydite for the format it is read against and when it is marked
// possibly stale.
//
// A comment is lydite's only when its author's login is exactly
// `<Options.LyditeApp>[bot]`, compared without case. Any other GitHub App,
// and github-actions[bot] from any workflow, can post a comment opening with
// the marker; one from any other author is ignored and gives no detail, so a
// forged comment cannot hand a session rows to fix.
//
// # Read failures
//
// A poll whose reads fail is no verdict: the next poll tries again. Three
// consecutive failed polls end the wait as could-not-read.
package prwait

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

// Reader is every read a poll makes. A token's githubapp.ReadClient
// satisfies it, and nothing in it writes.
type Reader interface {
	ReadPullRequest(ctx context.Context, number int) (githubapp.PullRequest, error)
	ReadCheckRuns(ctx context.Context, sha string) ([]githubapp.CheckRun, error)
	ReadCommitStatuses(ctx context.Context, sha string) (githubapp.CombinedStatus, error)
	ReadIssueComments(ctx context.Context, number int) ([]githubapp.IssueComment, error)
	ReadReviewThreads(ctx context.Context, number int) ([]githubapp.ReviewThread, error)
	// ReadViewer is the login of the account the token belongs to.
	ReadViewer(ctx context.Context) (string, error)
}

// Clock is where the wait reads the time and waits between polls.
type Clock interface {
	Now() time.Time
	// Sleep returns after d, or with ctx's error when ctx ends first.
	Sleep(ctx context.Context, d time.Duration) error
}

// RealClock is the process's own clock.
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time { return time.Now() }

// Sleep waits d, or until ctx ends.
func (RealClock) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Verdict is how a wait ended.
type Verdict string

const (
	Clean        Verdict = "clean"
	ActionNeeded Verdict = "action-needed"
	CapReached   Verdict = "cap-reached"
	HeadMoved    Verdict = "head-moved"
	Closed       Verdict = "closed"
	CouldNotRead Verdict = "could-not-read"
	NoChecks     Verdict = "no-checks"
)

// The exit code each verdict ends `agtk pr wait` with. 1 stays the code of a
// command that never started waiting: a malformed argument or flag.
const (
	ExitClean        = 0
	ExitActionNeeded = 2
	ExitCapReached   = 3
	ExitHeadMoved    = 4
	ExitClosed       = 5
	ExitCouldNotRead = 6
	ExitNoChecks     = 7
)

// ExitCode is the exit code v ends the command with.
func (v Verdict) ExitCode() int {
	switch v {
	case Clean:
		return ExitClean
	case ActionNeeded:
		return ExitActionNeeded
	case CapReached:
		return ExitCapReached
	case HeadMoved:
		return ExitHeadMoved
	case Closed:
		return ExitClosed
	case NoChecks:
		return ExitNoChecks
	}
	return ExitCouldNotRead
}

// MaxConsecutiveReadFailures is how many polls in a row may fail to read
// before the wait ends as could-not-read.
const MaxConsecutiveReadFailures = 3

// MaxEmptyPolls is how many judged polls may find the head with no check
// runs and no statuses before the wait ends as no-checks.
const MaxEmptyPolls = 2

// Options configures one wait.
type Options struct {
	// Number is the pull request's number.
	Number int
	// Poll is the wait between polls, and Cap the time after the wait
	// started past which a poll that is still pending ends it.
	Poll time.Duration
	Cap  time.Duration
	// Clock is RealClock when nil.
	Clock Clock
	// LyditeApp is the slug of the GitHub App that writes lydite's results
	// comment; only a comment by `<LyditeApp>[bot]` is read. DefaultLyditeApp
	// when empty.
	LyditeApp string
	// Progress, when set, is called once after every poll, including the
	// one that ends the wait.
	Progress func(PollReport)
}

// PollReport is one poll's outcome, for a progress line.
type PollReport struct {
	Poll           int
	Pending        int
	Failing        int
	WaitingThreads int
	// AnsweredThreads counts the unresolved threads the token's account
	// spoke last on.
	AnsweredThreads int
	// ReadErr is set when the poll's reads failed and nothing was judged.
	ReadErr error
	// Ended is Closed when the poll found the pull request not open, and
	// HeadMoved when it found a head other than the watched one; the head's
	// checks were then not judged. It is empty on every other poll.
	Ended Verdict
	// State is the pull request's state, set when Ended is Closed.
	State string
	// NewHeadSHA is the head found in place of the watched one, set when
	// Ended is HeadMoved.
	NewHeadSHA string
}

// Item is one check run or commit status the verdict names.
type Item struct {
	// Kind is "check-run", "status", or "none" for the placeholder that
	// stands for a head with nothing registered on it.
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	State string `json:"state"`
	URL   string `json:"url"`
}

// Thread is one review thread that is waiting.
type Thread struct {
	Path     string `json:"path"`
	Outdated bool   `json:"outdated"`
	// Excerpt is the first line of the thread's opening comment, cut to
	// excerptLimit runes.
	Excerpt string `json:"excerpt"`
}

// Result is how a wait ended and what it saw on its last judged poll.
type Result struct {
	Verdict Verdict
	// HeadSHA is the head the wait watched; NewHeadSHA is the one it moved
	// to, set only when Verdict is HeadMoved.
	HeadSHA    string
	NewHeadSHA string
	// State is the pull request's state as GitHub last reported it.
	State          string
	Polls          int
	Elapsed        time.Duration
	Failing        []Item
	Pending        []Item
	WaitingThreads []Thread
	// AnsweredUnresolved counts the unresolved threads whose latest comment
	// the token's account wrote: answered, and waiting on a person to
	// resolve them.
	AnsweredUnresolved int
	// Viewer is the login of the account the token belongs to, empty until
	// it has been read.
	Viewer string
	// ReferralPending reports that lydite/referral was pending on the head.
	ReferralPending bool
	// Lydite is the lydite comment's detail, set on an action-needed verdict
	// and on a clean one with a referral pending, when such a comment exists.
	Lydite *LyditeDetail
	// Err is the last read failure, set when Verdict is CouldNotRead.
	Err error
}

// referralContext is the one commit status a clean head may leave pending.
const referralContext = "lydite/referral"

// Wait polls the pull request until a poll reaches a verdict.
func Wait(ctx context.Context, reader Reader, opts Options) Result {
	clock := opts.Clock
	if clock == nil {
		clock = RealClock{}
	}
	lyditeApp := opts.LyditeApp
	if lyditeApp == "" {
		lyditeApp = DefaultLyditeApp
	}
	start := clock.Now()
	res := Result{Failing: []Item{}, Pending: []Item{}, WaitingThreads: []Thread{}}
	finish := func(v Verdict) Result {
		res.Verdict = v
		res.Elapsed = clock.Now().Sub(start)
		return res
	}
	var lyditeRunningAt time.Time
	failures, emptyPolls := 0, 0
	for {
		res.Polls++
		poll, err := read(ctx, reader, opts.Number, res.HeadSHA, &res.Viewer)
		if err == nil {
			res.State = poll.pr.State
			switch {
			case poll.pr.State != "open":
				report(opts, PollReport{Poll: res.Polls, Ended: Closed, State: poll.pr.State})
				return finish(Closed)
			case res.HeadSHA == "":
				res.HeadSHA = poll.pr.HeadSHA
			case poll.pr.HeadSHA != res.HeadSHA:
				res.NewHeadSHA = poll.pr.HeadSHA
				report(opts, PollReport{Poll: res.Polls, Ended: HeadMoved, NewHeadSHA: poll.pr.HeadSHA})
				return finish(HeadMoved)
			}
		}
		if err != nil {
			failures++
			res.Err = err
			report(opts, PollReport{Poll: res.Polls, ReadErr: err})
			if failures >= MaxConsecutiveReadFailures || ctx.Err() != nil {
				return finish(CouldNotRead)
			}
		} else {
			failures = 0
			res.Err = nil
			j := judge(poll, res.Viewer)
			res.Failing, res.Pending, res.WaitingThreads, res.ReferralPending = j.failing, j.pending, j.waiting, j.referral
			res.AnsweredUnresolved = j.answered
			now := clock.Now()
			if j.lyditeRunning {
				lyditeRunningAt = now
			}
			if j.empty {
				emptyPolls++
			} else {
				emptyPolls = 0
			}
			report(opts, PollReport{
				Poll: res.Polls, Pending: len(j.pending), Failing: len(j.failing),
				WaitingThreads: len(j.waiting), AnsweredThreads: j.answered,
			})
			switch {
			case len(j.failing) > 0 || len(j.waiting) > 0:
				res.Lydite = lyditeDetail(poll.comments, lyditeApp, lyditeRunningAt)
				return finish(ActionNeeded)
			case emptyPolls >= MaxEmptyPolls:
				return finish(NoChecks)
			case len(j.pending) == 0:
				if j.referral {
					res.Lydite = lyditeDetail(poll.comments, lyditeApp, lyditeRunningAt)
				}
				return finish(Clean)
			}
		}
		elapsed := clock.Now().Sub(start)
		if elapsed >= opts.Cap {
			return finish(CapReached)
		}
		if err := clock.Sleep(ctx, min(opts.Poll, opts.Cap-elapsed)); err != nil {
			res.Err = err
			return finish(CouldNotRead)
		}
	}
}

func report(opts Options, r PollReport) {
	if opts.Progress != nil {
		opts.Progress(r)
	}
}

// polled is everything one poll read.
type polled struct {
	pr       githubapp.PullRequest
	runs     []githubapp.CheckRun
	statuses githubapp.CombinedStatus
	comments []githubapp.IssueComment
	threads  []githubapp.ReviewThread
}

// read makes one poll's reads. The head's checks are read only once the pull
// request is known to be open and on the head being watched: a closed pull
// request or a moved head is a verdict with no need of them. The viewer is
// read while *viewer is empty, and a failure to read it fails the poll: a
// thread cannot be judged answered or waiting without it.
func read(ctx context.Context, reader Reader, number int, watched string, viewer *string) (polled, error) {
	var p polled
	var err error
	if p.pr, err = reader.ReadPullRequest(ctx, number); err != nil {
		return polled{}, fmt.Errorf("read pull request %d: %w", number, err)
	}
	if p.pr.State != "open" || (watched != "" && p.pr.HeadSHA != watched) {
		return p, nil
	}
	if *viewer == "" {
		login, err := reader.ReadViewer(ctx)
		if err != nil {
			return polled{}, fmt.Errorf("read the account the token belongs to: %w", err)
		}
		if login == "" {
			return polled{}, errors.New("GitHub reported no login for the account the token belongs to")
		}
		*viewer = login
	}
	if p.runs, err = reader.ReadCheckRuns(ctx, p.pr.HeadSHA); err != nil {
		return polled{}, fmt.Errorf("read the check runs on %s: %w", p.pr.HeadSHA, err)
	}
	if p.statuses, err = reader.ReadCommitStatuses(ctx, p.pr.HeadSHA); err != nil {
		return polled{}, fmt.Errorf("read the commit statuses on %s: %w", p.pr.HeadSHA, err)
	}
	if p.comments, err = reader.ReadIssueComments(ctx, number); err != nil {
		return polled{}, fmt.Errorf("read the comments on pull request %d: %w", number, err)
	}
	if p.threads, err = reader.ReadReviewThreads(ctx, number); err != nil {
		return polled{}, fmt.Errorf("read the review threads on pull request %d: %w", number, err)
	}
	return p, nil
}

// judgement is one poll's reading of the head.
type judgement struct {
	failing, pending []Item
	waiting          []Thread
	// answered counts the unresolved threads the viewer spoke last on.
	answered int
	// empty reports a head with no check runs and no statuses at all.
	empty    bool
	referral bool
	// lyditeRunning reports a lydite check or status, other than
	// lydite/referral, still running on the head.
	lyditeRunning bool
}

// passingConclusions are the check run conclusions a clean head may carry.
var passingConclusions = map[string]bool{"success": true, "neutral": true, "skipped": true}

func judge(p polled, viewer string) judgement {
	j := judgement{failing: []Item{}, pending: []Item{}, waiting: []Thread{}}
	for _, run := range p.runs {
		url := run.HTMLURL
		if url == "" {
			url = run.DetailsURL
		}
		switch {
		case run.Status != "completed":
			j.pending = append(j.pending, Item{Kind: "check-run", Name: run.Name, State: run.Status, URL: url})
			if isLydite(run.Name) {
				j.lyditeRunning = true
			}
		case !passingConclusions[run.Conclusion]:
			j.failing = append(j.failing, Item{Kind: "check-run", Name: run.Name, State: run.Conclusion, URL: url})
		}
	}
	for _, status := range p.statuses.Statuses {
		item := Item{Kind: "status", Name: status.Context, State: status.State, URL: status.TargetURL}
		switch {
		case status.State == "success":
		case status.State == "pending" && status.Context == referralContext:
			j.referral = true
		case status.State == "pending":
			j.pending = append(j.pending, item)
			if isLydite(status.Context) {
				j.lyditeRunning = true
			}
		default:
			j.failing = append(j.failing, item)
		}
	}
	if len(p.runs) == 0 && len(p.statuses.Statuses) == 0 {
		j.empty = true
		j.pending = append(j.pending, Item{Kind: "none", Name: "no checks registered on the head", State: "pending"})
	}
	for _, thread := range p.threads {
		switch {
		case thread.Resolved:
		case answeredBy(thread, viewer):
			j.answered++
		default:
			j.waiting = append(j.waiting, Thread{Path: thread.Path, Outdated: thread.Outdated, Excerpt: excerpt(thread.Body)})
		}
	}
	return j
}

// answeredBy reports whether viewer wrote the thread's latest comment.
// Logins compare without case, as GitHub matches them; an empty login, a
// deleted account's, never matches.
func answeredBy(thread githubapp.ReviewThread, viewer string) bool {
	return thread.LastAuthor != "" && viewer != "" && strings.EqualFold(thread.LastAuthor, viewer)
}

// isLydite reports whether a check run name or status context is lydite's.
func isLydite(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), "lydite")
}

// excerptLimit bounds a thread excerpt, in runes.
const excerptLimit = 120

func excerpt(body string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	line = strings.TrimSpace(line)
	if runes := []rune(line); len(runes) > excerptLimit {
		return string(runes[:excerptLimit]) + "…"
	}
	return line
}
