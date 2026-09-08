package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/review"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewpost"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun"
)

// pullRequestTarget is an open pull request, resolved to everything a review
// of it needs: where to post, which commits to measure between, and where
// GitHub will accept an inline comment.
type pullRequestTarget struct {
	slug   review.Slug
	pr     githubapp.PullRequest
	client *githubapp.Client
	// mergeBase is where the base and the head diverged, which is what the
	// change is measured from.
	mergeBase string
	// added is which lines of the diff a comment may be attached to.
	added reviewpost.AddedLines
}

// clientSeam is where the registration and the network come from.
//
// A seam rather than a direct call, for the reason internal/reviewrun makes
// one for the driver: resolving a pull request is the step that reads GitHub,
// fetches the head and works out where a comment may land, and none of it
// needs a real App or a real network to be exercised. The zero value is the
// real thing.
type clientSeam struct {
	// dir is the registration directory, or "" for this machine's own.
	dir string
	// doer is the transport, or nil for the network.
	doer githubapp.Doer
}

// client builds the client this seam describes.
func (s clientSeam) client(slug review.Slug) (*githubapp.Client, error) {
	dir := s.dir
	if dir == "" {
		resolved, err := githubapp.Dir()
		if err != nil {
			return nil, err
		}
		dir = resolved
	}
	cred, err := githubapp.Load(dir)
	if err != nil {
		return nil, err
	}
	var opts []githubapp.Option
	if s.doer != nil {
		opts = append(opts, githubapp.WithHTTP(s.doer))
	}
	return githubapp.NewClient(cred, slug.String(), opts...), nil
}

// resolvePullRequest reads a pull request and brings its commits into the
// local repository.
//
// The head arrives by its commit id, and every ref the pipeline is then
// pointed at is that id rather than the branch name the pull request carries.
// A branch name is chosen by the change's author and can move between the read
// and the review; a review is bound to a commit, so the commit is what the
// whole run is anchored to.
func resolvePullRequest(ctx context.Context, root string, number int, seam clientSeam) (*pullRequestTarget, error) {
	slug, err := review.RemoteSlug(root, review.DefaultRemote)
	if err != nil {
		return nil, err
	}
	client, err := seam.client(slug)
	if err != nil {
		return nil, err
	}

	pr, err := client.ReadPullRequest(ctx, number)
	if err != nil {
		return nil, err
	}
	if _, err := review.FetchPullRequest(root, number, pr.HeadSHA); err != nil {
		return nil, err
	}
	if err := review.FetchCommit(root, pr.BaseSHA); err != nil {
		return nil, err
	}
	mergeBase, err := review.MergeBase(root, pr.BaseSHA, pr.HeadSHA)
	if err != nil {
		return nil, fmt.Errorf("anchor pull request %d to %s: %w", number, pr.BaseRef, err)
	}
	added, err := review.AddedLinesAt(root, mergeBase, pr.HeadSHA)
	if err != nil {
		return nil, fmt.Errorf("read the diff of pull request %d: %w", number, err)
	}
	return &pullRequestTarget{
		slug: slug, pr: pr, client: client,
		mergeBase: mergeBase,
		added:     added,
	}, nil
}

// priorThreads reads the comment threads the pull request already carries.
//
// A read that fails is reported rather than fatal, and it is reported as a read
// that failed rather than as a pull request holding nothing. Both end in
// "nothing was withheld", and only one of them means the pull request is clean
// — so refusing the review outright would make one GraphQL outage the end of
// reviewing, and treating the failure as an empty list would silently repost
// every finding somebody has already answered.
//
// A failed reviews query folds in here. If it did not answer, this run does not
// know what the pull request holds, and saying that once is the honest report.
func priorThreads(ctx context.Context, t *pullRequestTarget, reviewsErr error) reviewrun.Threads {
	if reviewsErr != nil {
		return reviewrun.ThreadsUnreadable("%v", reviewsErr)
	}
	threads, err := t.client.ReadReviewThreads(ctx, t.pr.Number)
	if err != nil {
		return reviewrun.ThreadsUnreadable("%v", err)
	}
	return reviewpost.ReadThreads(threads)
}

// reportUnchangedHead says that this commit already carries a review, and that
// re-deriving what the pull request already displays is what was avoided.
func reportUnchangedHead(env *Env, t *pullRequestTarget, asJSON bool) error {
	if asJSON {
		return writeJSON(env, unchangedHeadJSON(t))
	}
	fmt.Fprintf(env.Stdout, "%s#%d already carries a review of %s.\n", t.slug, t.pr.Number, t.pr.HeadSHA)
	fmt.Fprintln(env.Stdout, "No panel ran: nothing was spent and nothing was posted.")
	fmt.Fprintln(env.Stdout, "Push a commit to review what changed, or pass --force to review this one again.")
	return nil
}

// options builds the review options that point the pipeline at this pull
// request.
//
// The context is the posting one, which is what makes the manifest, the
// prompts and the convention documents come from the base ref rather than
// from the branch under review — the closure ADR 0007 makes structural.
func (t *pullRequestTarget) options(root string, target reviewTarget, flags runFlags, threads reviewrun.Threads) reviewrun.Options {
	return reviewrun.Options{
		Threads:     threads,
		Dir:         root,
		Base:        t.mergeBase,
		BaseLabel:   t.pr.BaseRef,
		Head:        t.pr.HeadSHA,
		Context:     review.ContextPR,
		Panel:       target.panel,
		Timeout:     flags.timeout,
		MaxParallel: flags.maxParallel,
	}
}

// renderEnvelope writes what a review of this pull request would be posted as,
// before a panel has run and therefore before any finding exists.
//
// The comment list is named as pending rather than shown as empty: a preview
// that printed an empty list would read as "this review would post nothing",
// which is the one thing a review must never say by accident.
func renderEnvelope(w io.Writer, t *pullRequestTarget, threads reviewrun.Threads) {
	fmt.Fprintf(w, "\nWould post one review to %s#%d, and nothing else:\n", t.slug, t.pr.Number)
	fmt.Fprintf(w, "  POST /repos/%s/pulls/%d/reviews\n", t.slug, t.pr.Number)
	fmt.Fprintf(w, "  commit_id: %s\n", t.pr.HeadSHA)
	fmt.Fprintf(w, "  event:     %s\n", githubapp.EventComment)
	fmt.Fprintf(w, "  comments:  (one per surviving finding that lands on the diff, supplied once the panel has answered)\n")
	fmt.Fprintf(w, "\n%d file(s) in this diff carry a line an inline comment could be attached to.\n", len(t.added))
	renderThreadRead(w, threads)
	fmt.Fprintln(w, "Nothing was spent and nothing was posted.")
}

// renderThreadRead says what the pull request already carries, for a preview
// that has no review to say it through.
//
// A preview that omitted a failed read would show the request a run would make
// while withholding the one thing that changes what is in it: with no threads
// read, nothing is withheld, and the review repeats what the pull request
// already carries.
func renderThreadRead(w io.Writer, threads reviewrun.Threads) {
	if threads.Available {
		fmt.Fprintf(w, "%d comment thread(s) are already on this pull request, %d of them open. A finding one of them already carries is not posted again.\n",
			threads.Count(), len(threads.Open()))
		return
	}
	fmt.Fprintf(w, "The existing comment threads could not be read: %s\n", threads.Reason)
	fmt.Fprintln(w, "A review run now would withhold nothing, so it may repeat what the pull request already carries.")
}

// renderPayload writes the exact request a post would make.
func renderPayload(w io.Writer, t *pullRequestTarget, payload githubapp.ReviewPayload, place reviewpost.Placement) {
	fmt.Fprintf(w, "\nPOST /repos/%s/pulls/%d/reviews\n", t.slug, t.pr.Number)
	fmt.Fprintf(w, "commit_id: %s\n", payload.CommitID)
	fmt.Fprintf(w, "event:     %s\n", payload.Event)
	fmt.Fprintf(w, "comments:  %d\n\n", len(payload.Comments))
	for _, c := range payload.Comments {
		fmt.Fprintf(w, "--- %s:%s (%s) ---\n%s\n", c.Path, commentRange(c), c.Side, c.Body)
	}
	fmt.Fprintf(w, "--- body ---\n%s\n", payload.Body)
	renderPlacement(w, place)
	fmt.Fprintln(w, "Nothing was posted.")
}

// renderPlacement says what became of every finding, so a reader can tell an
// empty comment list from a review that had nothing to say.
func renderPlacement(w io.Writer, place reviewpost.Placement) {
	fmt.Fprintf(w, "\n%d finding(s): %d inline, %d with no line, %d outside this diff.\n",
		place.Total(), len(place.Inline), len(place.CrossCutting), len(place.Unpositioned))
	for _, f := range place.Unpositioned {
		// The anchor rather than the start. A comment is attached at the end
		// of its region, so that is the line GitHub validates and the line a
		// finding is refused for — naming the start would report a line that
		// is on the diff as the reason the finding is not on it.
		fmt.Fprintf(w, "  moved to the body — %s:%d is not a line this pull request adds, and one comment GitHub refuses discards the whole review\n",
			f.Path, reviewpost.AnchorLine(f))
	}
}

// commentRange renders the lines an inline comment spans.
func commentRange(c githubapp.ReviewComment) string {
	if c.StartLine != nil {
		return fmt.Sprintf("%d-%d", *c.StartLine, c.Line)
	}
	return fmt.Sprintf("%d", c.Line)
}

// runCodeReviewPR reviews an open pull request and posts one review to it.
//
// Producing the review and transmitting it are separate steps here for the
// reason ADR 0006 gives: a panel costs real money, so a transport failure has
// to be retryable without re-running it, and the review is complete in memory
// before anything is sent.
func runCodeReviewPR(cmd *cobra.Command, env *Env, target reviewTarget, flags runFlags, seam clientSeam) error {
	if err := checkPullRequestFlags(target, cmd.Flags().Changed("context")); err != nil {
		return err
	}
	if flags.dryRun && flags.noPost {
		return errors.New("--dry-run spends nothing and --no-post runs the panel and withholds only the post; pass one")
	}
	root, err := review.RepoRoot(env.WorkDir)
	if err != nil {
		return fmt.Errorf("locate the repository: %w", err)
	}
	t, err := resolvePullRequest(cmd.Context(), root, target.pr, seam)
	if err != nil {
		return err
	}

	// Read before a reviewer is started. A head that already carries a review
	// costs one query to recognise and a whole panel to rediscover, and what
	// the panel would rediscover is what the pull request is already
	// displaying.
	reviewed, reviewsErr := t.client.ReadReviewedCommits(cmd.Context(), t.pr.Number)
	if !flags.dryRun && !flags.force && reviewsErr == nil && reviewed[t.pr.HeadSHA] {
		return reportUnchangedHead(env, t, flags.json)
	}
	opts := t.options(root, target, flags, priorThreads(cmd.Context(), t, reviewsErr))

	if flags.dryRun {
		opts.Preview = true
		plan, _, _, reviewRoot, err := reviewrun.Prepare(opts)
		if err != nil {
			return err
		}
		defer func() { _ = reviewRoot.Close() }()
		if flags.json {
			return writeJSON(env, pullRequestPlanJSON(t, plan, opts.Threads))
		}
		reviewrun.RenderPlan(env.Stdout, plan)
		renderEnvelope(env.Stdout, t, opts.Threads)
		return nil
	}

	result, err := reviewrun.Run(cmd.Context(), opts)
	if err != nil {
		return err
	}
	payload, place := reviewpost.Build(result, t.pr, t.added)

	if flags.noPost {
		if err := reportReview(env, t, result, payload, place, nil, flags.json); err != nil {
			return err
		}
		return unavailableError(result)
	}

	posted, err := t.client.CreateReview(cmd.Context(), t.pr.Number, payload)
	if err != nil {
		// The review is not lost with the post. Reporting it costs nothing and
		// is the difference between a rate limit that wasted a panel and one
		// that wasted a request. It is reported in whichever form the caller
		// asked for: a --json consumer parsing this stream must not be handed
		// prose because the post is what failed.
		if reportErr := reportReview(env, t, result, payload, place, nil, flags.json); reportErr != nil {
			return reportErr
		}
		return fmt.Errorf("post the review to %s#%d: %w", t.slug, t.pr.Number, err)
	}

	if err := reportReview(env, t, result, payload, place, &posted, flags.json); err != nil {
		return err
	}
	return unavailableError(result)
}

// reportReview writes what the review says and what became of it, in whichever
// form the caller asked for.
//
// One function rather than a branch at each of the three call sites, because
// the three differ only in whether the review was posted — and a --json branch
// that returned early is how the exit status and the output format came apart
// from the rest of the command in the first place.
func reportReview(env *Env, t *pullRequestTarget, result *reviewrun.Review,
	payload githubapp.ReviewPayload, place reviewpost.Placement,
	posted *githubapp.PostedReview, asJSON bool,
) error {
	if asJSON {
		return writeJSON(env, pullRequestPostJSON(t, result, payload, place, posted))
	}
	reviewrun.Render(env.Stdout, result)
	if posted == nil {
		renderPayload(env.Stdout, t, payload, place)
		return nil
	}
	fmt.Fprintf(env.Stdout, "\nPosted one review to %s#%d: %s\n", t.slug, t.pr.Number, posted.HTMLURL)
	renderPlacement(env.Stdout, place)
	return nil
}

// checkPullRequestFlags refuses a target that names a pull request and then
// contradicts it.
//
// A pull request decides the base, the head and the context. Accepting a
// second answer for any of them would mean reviewing one change and posting
// the result to another.
func checkPullRequestFlags(target reviewTarget, contextNamed bool) error {
	if target.pr < 1 {
		return fmt.Errorf("%d is not a pull request number", target.pr)
	}
	for _, named := range []struct{ flag, value string }{
		{"--base", target.base},
		{"--head", target.head},
	} {
		if named.value != "" {
			return fmt.Errorf("--pr names the change to review; %s names a different one, so pass one or the other", named.flag)
		}
	}
	// The context is only checked when somebody named one. A review of a pull
	// request always runs in the context that posts — that is what makes its
	// manifest and its prompts come from the base ref — so the flag's default
	// is not a second answer to weigh, and only an explicit contradiction is.
	if contextNamed && review.Context(target.context) != review.ContextPR {
		return fmt.Errorf("--pr reviews a pull request, which is the %q context; --context %s says otherwise, so pass one or the other",
			review.ContextPR, target.context)
	}
	return nil
}

// unavailableError reports a review that could not reach a verdict.
//
// Findings never make the command fail: what a review found is the review's
// content, and a severity floor that blocks is a property of approval rather
// than of running a panel.
func unavailableError(r *reviewrun.Review) error {
	if !r.Available {
		return fmt.Errorf("the review could not reach a verdict: %s", r.Reason)
	}
	return nil
}
