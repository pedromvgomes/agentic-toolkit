package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/prwait"
)

// exitCodeError ends the command with code. When err is set it is printed
// the way any other command error is; when it is nil the command has already
// written everything it has to say.
type exitCodeError struct {
	code int
	err  error
}

func (e *exitCodeError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("exit status %d", e.code)
	}
	return e.err.Error()
}

func (e *exitCodeError) Unwrap() error { return e.err }

func newPRCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Watch a pull request",
		// Without a RunE, cobra takes an unknown subcommand as an argument,
		// prints help and exits 0.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newPRWaitCmd(env, clientSeam{}))
	return cmd
}

// parsePullRequestRef reads "owner/repo#N", where N is a positive pull
// request number written in plain decimal.
func parsePullRequestRef(s string) (string, int, error) {
	slug, num, ok := strings.Cut(s, "#")
	if !ok {
		return "", 0, fmt.Errorf("%q is not a pull request: want owner/repo#N", s)
	}
	repo, err := parseBacklogRepo(slug)
	if err != nil {
		return "", 0, fmt.Errorf("%q is not a pull request: want owner/repo#N", s)
	}
	if !backlogNumRE.MatchString(num) {
		return "", 0, fmt.Errorf("%q is not a pull request: the number must be a positive integer", s)
	}
	number, err := strconv.Atoi(num)
	if err != nil {
		return "", 0, fmt.Errorf("%q is not a pull request: the number is out of range", s)
	}
	return repo, number, nil
}

// prWaitJSON is the one object `agtk pr wait --json` writes. Every array is
// written as [] when empty, never null.
type prWaitJSON struct {
	Verdict        prwait.Verdict  `json:"verdict"`
	HeadSHA        string          `json:"head_sha"`
	NewHeadSHA     string          `json:"new_head_sha,omitempty"`
	State          string          `json:"state,omitempty"`
	Polls          int             `json:"polls"`
	ElapsedSeconds int64           `json:"elapsed_seconds"`
	Failing        []prwait.Item   `json:"failing"`
	Pending        []prwait.Item   `json:"pending"`
	WaitingThreads []prwait.Thread `json:"waiting_threads"`
	// AnsweredUnresolved counts the unresolved threads the token's account
	// spoke last on, which never hold the wait.
	AnsweredUnresolved int                  `json:"answered_unresolved"`
	ReferralPending    bool                 `json:"referral_pending"`
	Lydite             *prwait.LyditeDetail `json:"lydite,omitempty"`
	Error              string               `json:"error,omitempty"`
}

func newPRWaitCmd(env *Env, seam clientSeam) *cobra.Command {
	var (
		poll, capAfter time.Duration
		jsonOut        bool
	)
	cmd := &cobra.Command{
		Use:   "wait owner/repo#N",
		Short: "Poll a pull request's checks and threads until it is clean, needs action, or the cap runs out",
		Long: "Polls a pull request every --poll until one poll reaches a verdict, and\n" +
			"exits once with that verdict's code. Each poll reads the pull request, the\n" +
			"check runs and commit statuses of its head, its comments and its review\n" +
			"threads. Nothing is written to GitHub.\n" +
			"\n" +
			"An unresolved review thread is answered when its latest comment was\n" +
			"written by the account the token belongs to, and waiting otherwise,\n" +
			"outdated or not; a thread whose latest author is a deleted account is\n" +
			"waiting. Answered threads are counted and never hold the wait.\n" +
			"\n" +
			"It reads with the token in GH_TOKEN or GITHUB_TOKEN, and never as an App\n" +
			"registration, even on a machine that holds one.\n" +
			"\n" +
			"Exit codes:\n" +
			"  0  clean: every check run passed (success, neutral or skipped), every\n" +
			"     commit status is success except lydite/referral, which may stay\n" +
			"     pending, and no review thread is waiting\n" +
			"  2  action needed: a check run or status failed, or a review thread is\n" +
			"     waiting; each is named\n" +
			"  3  cap reached: --cap passed with something still pending; each is named\n" +
			"  4  head moved: the head is no longer the one the first poll saw; run the\n" +
			"     command again to wait on the new head with a fresh cap\n" +
			"  5  closed: the pull request is closed or merged\n" +
			"  6  could not read: no token, or reads failed on 3 polls in a row\n" +
			"  7  no checks: the head still had no check runs and no statuses on a\n" +
			"     second poll; one empty poll right after a push stays pending\n" +
			"  1  the command did not start: a malformed argument or flag\n" +
			"\n" +
			"A poll whose reads fail is retried at the next poll. lydite's results\n" +
			"comment is reported as detail beside an action-needed verdict or a clean\n" +
			"one with a referral pending; the head's checks decide every verdict.\n" +
			"\n" +
			"One line per poll goes to stderr, and the verdict to stdout. With --json,\n" +
			"stdout is one object: {verdict, head_sha, polls, elapsed_seconds, failing,\n" +
			"pending, waiting_threads, answered_unresolved, referral_pending}, with\n" +
			"lydite and error when they apply. Every string GitHub supplied is printed\n" +
			"quoted in text output.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, number, err := parsePullRequestRef(args[0])
			if err != nil {
				return err
			}
			if poll <= 0 || capAfter <= 0 {
				return fmt.Errorf("--poll and --cap must be positive durations, got %s and %s", poll, capAfter)
			}
			token, _ := seam.token()
			if token == "" {
				err := fmt.Errorf("no GitHub token to read %s#%d with: set GH_TOKEN or GITHUB_TOKEN to a token that can read its pull requests and checks", repo, number)
				if jsonOut {
					if jerr := writeJSON(env, prWaitJSON{
						Verdict: prwait.CouldNotRead, Failing: []prwait.Item{}, Pending: []prwait.Item{},
						WaitingThreads: []prwait.Thread{}, Error: err.Error(),
					}); jerr != nil {
						return jerr
					}
				}
				return &exitCodeError{code: prwait.ExitCouldNotRead, err: err}
			}
			reader, err := githubapp.NewReadClient(token, repo, seam.options()...)
			if err != nil {
				return &exitCodeError{code: prwait.ExitCouldNotRead, err: err}
			}
			res := prwait.Wait(cmd.Context(), reader, prwait.Options{
				Number: number, Poll: poll, Cap: capAfter,
				Progress: func(r prwait.PollReport) { printPollReport(env.Stderr, r) },
			})
			if jsonOut {
				if err := writeJSON(env, prWaitResultJSON(res)); err != nil {
					return err
				}
			} else {
				printPRWaitResult(env.Stdout, repo, number, res)
			}
			if code := res.Verdict.ExitCode(); code != prwait.ExitClean {
				return &exitCodeError{code: code}
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&poll, "poll", 5*time.Minute, "the wait between polls")
	cmd.Flags().DurationVar(&capAfter, "cap", 2*time.Hour, "how long after starting a still-pending wait ends")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit the verdict as one JSON object on stdout")
	return cmd
}

func prWaitResultJSON(res prwait.Result) prWaitJSON {
	out := prWaitJSON{
		Verdict:            res.Verdict,
		HeadSHA:            res.HeadSHA,
		NewHeadSHA:         res.NewHeadSHA,
		State:              res.State,
		Polls:              res.Polls,
		ElapsedSeconds:     int64(res.Elapsed / time.Second),
		Failing:            res.Failing,
		Pending:            res.Pending,
		WaitingThreads:     res.WaitingThreads,
		AnsweredUnresolved: res.AnsweredUnresolved,
		ReferralPending:    res.ReferralPending,
		Lydite:             res.Lydite,
	}
	if out.Failing == nil {
		out.Failing = []prwait.Item{}
	}
	if out.Pending == nil {
		out.Pending = []prwait.Item{}
	}
	if out.WaitingThreads == nil {
		out.WaitingThreads = []prwait.Thread{}
	}
	if res.Err != nil {
		out.Error = res.Err.Error()
	}
	return out
}

// printPollReport writes one progress line. A read failure's message carries
// text GitHub supplied, so it is quoted whole.
func printPollReport(w io.Writer, r prwait.PollReport) {
	if r.ReadErr != nil {
		fmt.Fprintf(w, "poll %d: could not read: %q\n", r.Poll, r.ReadErr.Error())
		return
	}
	fmt.Fprintf(w, "poll %d: %d pending, %d failing, %d waiting threads, %d answered\n",
		r.Poll, r.Pending, r.Failing, r.WaitingThreads, r.AnsweredThreads)
}

// printPRWaitResult writes the verdict block. Every string GitHub supplied
// is printed with %q, so none can start a line of its own.
func printPRWaitResult(w io.Writer, repo string, number int, res prwait.Result) {
	fmt.Fprintf(w, "%s#%d: %s\n", repo, number, res.Verdict)
	fmt.Fprintf(w, "head: %q\n", res.HeadSHA)
	if res.NewHeadSHA != "" {
		fmt.Fprintf(w, "new head: %q\n", res.NewHeadSHA)
	}
	if res.Verdict == prwait.Closed {
		fmt.Fprintf(w, "state: %q\n", res.State)
	}
	fmt.Fprintf(w, "polls: %d, elapsed: %s\n", res.Polls, res.Elapsed.Round(time.Second))
	if res.Err != nil {
		fmt.Fprintf(w, "error: %q\n", res.Err.Error())
	}
	printItems(w, "failing", res.Failing)
	printItems(w, "pending", res.Pending)
	if len(res.WaitingThreads) > 0 {
		fmt.Fprintln(w, "waiting threads:")
		for _, t := range res.WaitingThreads {
			outdated := ""
			if t.Outdated {
				outdated = " (outdated)"
			}
			fmt.Fprintf(w, "  %q%s: %q\n", t.Path, outdated, t.Excerpt)
		}
	}
	if res.AnsweredUnresolved > 0 {
		fmt.Fprintf(w, "answered unresolved threads: %d, waiting on a person to resolve them\n", res.AnsweredUnresolved)
	}
	if res.ReferralPending {
		fmt.Fprintln(w, "referral pending: a human clears it by commenting `/lydite clear`")
	}
	if l := res.Lydite; l != nil {
		stale := ""
		if l.PossiblyStale {
			stale = " (possibly stale: lydite was still running on this head after it was written)"
		}
		fmt.Fprintf(w, "lydite: %s %q, updated %s%s\n", l.Verdict, l.Heading, l.UpdatedAt.UTC().Format(time.RFC3339), stale)
		for _, row := range l.Rows {
			fmt.Fprintf(w, "  %s %q: %q\n", row.Mark, row.Section, row.Text)
		}
	}
}

func printItems(w io.Writer, label string, items []prwait.Item) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(w, "%s:\n", label)
	for _, item := range items {
		fmt.Fprintf(w, "  %s %q: %q %q\n", item.Kind, item.Name, item.State, item.URL)
	}
}
