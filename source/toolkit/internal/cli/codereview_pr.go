package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/relay"
	"github.com/pedromvgomes/agentic-toolkit/internal/review"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewapprove"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewpost"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun"
)

// pullRequestTarget is an open pull request, resolved to everything a review
// of it needs: where to post, which commits to measure between, and where
// GitHub will accept an inline comment.
type pullRequestTarget struct {
	slug review.Slug
	pr   githubapp.PullRequest
	// reader is what the pull request's reviews and threads are read through.
	reader pullRequestReader
	// client is the App's, and is what posts. It is nil when the pull request
	// was read with a token: a review posted under any identity but the App's
	// is invisible to the reads that decide which head was reviewed and what
	// approval may count, so a token reads and never posts.
	client *githubapp.Client
	// mergeBase is where the base and the head diverged, which is what the
	// change is measured from.
	mergeBase string
	// added is which lines of the diff a comment may be attached to.
	//
	// Always the whole change, however much of it a review reads: GitHub
	// places a comment against the pull request's own diff, so a finding from
	// a narrowed review lands on the same lines one from a full review would.
	added reviewpost.AddedLines
	// scope is how much of the change this review reads. The zero value is
	// the whole of it.
	scope reviewScope
}

// reviewScope is how much of a pull request a review reads.
//
// A re-review after a push reads on from the last review this installation
// posted that reached a verdict, when that is safe, and the whole change
// otherwise. The earlier review's findings stand on their own threads and
// approval walks back to it through the marker — ADR 0018.
type reviewScope struct {
	// since is the head of the review this one reads on from, and is empty
	// when the review reads the whole change.
	since string
	// reason says why the whole change is read, and is empty when it is not.
	reason string
}

// describe says what a review in this scope reads, for a line of output.
func (s reviewScope) describe() string {
	if s.since != "" {
		return fmt.Sprintf("delta since %s, the last head a review by this installation reached a verdict on", s.since)
	}
	return fmt.Sprintf("full change (%s)", s.reason)
}

// kind names the scope for a JSON consumer branching on it.
func (s reviewScope) kind() string {
	if s.since != "" {
		return "delta"
	}
	return "full"
}

// chooseScope decides how much of the pull request this review reads.
//
// Narrowing is the exception, taken only when every condition that makes it
// safe holds; any doubt reads the whole change. Each condition closes a way the
// earlier review could stop describing the code:
//
//   - its head must be an ancestor of this one, or the branch was rewritten
//     and what it read is no longer part of the change;
//   - the base must not have been merged in since, or Since..Head carries the
//     base branch's commits and the review reads code nobody on this branch
//     wrote.
func chooseScope(root string, t *pullRequestTarget, reviews []githubapp.SubmittedReview, reviewsErr error, full bool) reviewScope {
	if full {
		return reviewScope{reason: "--full was passed"}
	}
	if reviewsErr != nil {
		return reviewScope{reason: "the reviews on this pull request could not be read"}
	}
	previous, found := reviewapprove.LastCompleteReview(reviews)
	if !found {
		return reviewScope{reason: "no earlier review by this installation reached a verdict"}
	}
	since := previous.Head
	if since == t.pr.HeadSHA {
		return reviewScope{reason: "the last complete review is of this head"}
	}
	if err := review.FetchCommit(root, since); err != nil {
		return reviewScope{reason: fmt.Sprintf("%s, the last reviewed head, is not in the repository", since)}
	}
	if !review.IsAncestor(root, since, t.pr.HeadSHA) {
		return reviewScope{reason: fmt.Sprintf("%s, the last reviewed head, is not an ancestor of this one: the branch was rewritten", since)}
	}
	mergeBase, err := review.MergeBase(root, t.pr.BaseSHA, since)
	if err != nil || mergeBase != t.mergeBase {
		return reviewScope{reason: "the base branch was merged in since the last complete review"}
	}
	return reviewScope{since: since}
}

// diffBase is the commit this review's diff starts at.
func (t *pullRequestTarget) diffBase() string {
	if t.scope.since != "" {
		return t.scope.since
	}
	return t.mergeBase
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
	// getenv reads the environment a token is looked for in, or is nil for
	// this process's own.
	getenv func(string) string
}

// pullRequestReader is what reading a pull request takes, and nothing that
// writes. The App's client and a token's githubapp.ReadClient both satisfy it.
type pullRequestReader interface {
	ReadPullRequest(ctx context.Context, number int) (githubapp.PullRequest, error)
	ReadReviewThreads(ctx context.Context, number int) ([]githubapp.ReviewThread, error)
	ReadSubmittedReviews(ctx context.Context, number int) ([]githubapp.SubmittedReview, error)
}

// tokenVariables are where a token to read with is looked for, in order.
var tokenVariables = []string{"GH_TOKEN", "GITHUB_TOKEN"}

// token returns the first of tokenVariables that holds more than whitespace,
// and the name of the variable it came from. Both are empty when none does.
func (s clientSeam) token() (token, variable string) {
	getenv := s.getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	for _, name := range tokenVariables {
		if value := strings.TrimSpace(getenv(name)); value != "" {
			return value, name
		}
	}
	return "", ""
}

// relayVariable names the relay repository a posting command on a machine
// holding no registration hands its post to.
const relayVariable = "AGTK_CODE_REVIEW_RELAY"

// relay returns the relay repository relayVariable names, and the name of the
// variable it came from. Both are empty when it holds only whitespace.
func (s clientSeam) relay() (repo, variable string) {
	getenv := s.getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if value := strings.TrimSpace(getenv(relayVariable)); value != "" {
		return value, relayVariable
	}
	return "", ""
}

// relayDoer is the transport this seam names, for reaching the relay.
//
// With no transport named it is a client holding the same per-request bound
// githubapp's own client does, so a relay that stops answering fails one
// request rather than hanging the wait.
func (s clientSeam) relayDoer() relay.Doer {
	if s.doer == nil {
		return &http.Client{Timeout: 30 * time.Second}
	}
	return s.doer
}

// options is the transport this seam names, for either kind of client.
func (s clientSeam) options() []githubapp.Option {
	if s.doer == nil {
		return nil
	}
	return []githubapp.Option{githubapp.WithHTTP(s.doer)}
}

// client builds the App's client from this machine's registration.
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
	return githubapp.NewClient(cred, slug.String(), s.options()...), nil
}

// resolvePullRequest reads a pull request as the App and brings its commits
// into the local repository.
//
// It is what every command that posts resolves through, and it refuses on a
// machine holding no registration whatever the environment carries. The
// refusal comes before anything is read, so a run that could never post its
// review never spends a panel on one.
func resolvePullRequest(ctx context.Context, root string, number int, seam clientSeam) (*pullRequestTarget, error) {
	slug, err := review.RemoteSlug(root, review.DefaultRemote)
	if err != nil {
		return nil, err
	}
	client, err := seam.client(slug)
	if err != nil {
		return nil, err
	}
	return readAsApp(ctx, root, number, slug, client)
}

// readAsApp reads a pull request through the App's client, which the target
// then both reads and posts through.
func readAsApp(ctx context.Context, root string, number int, slug review.Slug, client *githubapp.Client) (*pullRequestTarget, error) {
	pr, err := client.ReadPullRequest(ctx, number)
	if err != nil {
		return nil, err
	}
	t, err := anchorPullRequest(root, number, slug, pr)
	if err != nil {
		return nil, err
	}
	t.reader, t.client = client, client
	return t, nil
}

// readPullRequest resolves a pull request for a command that posts nothing.
//
// It reads as the App when this machine is registered, and otherwise with a
// token from GH_TOKEN or GITHUB_TOKEN. Only a machine holding no registration
// at all falls back: one holding a broken or half-written registration is
// refused with what fixes it, because reading under another identity would
// hide the breakage until the first command that posts.
func readPullRequest(ctx context.Context, root string, number int, seam clientSeam) (*pullRequestTarget, error) {
	slug, err := review.RemoteSlug(root, review.DefaultRemote)
	if err != nil {
		return nil, err
	}
	client, err := seam.client(slug)
	if err == nil {
		return readAsApp(ctx, root, number, slug, client)
	}
	if !githubapp.Unregistered(err) {
		return nil, err
	}
	token, variable := seam.token()
	if token == "" {
		return nil, fmt.Errorf("%w, or set GH_TOKEN or GITHUB_TOKEN to a token that can read %s", err, slug)
	}
	reader, err := githubapp.NewReadClient(token, slug.String(), seam.options()...)
	if err != nil {
		return nil, err
	}
	pr, err := reader.ReadPullRequest(ctx, number)
	if err != nil {
		// GitHub answers a token that cannot see a repository as though the
		// repository were not there, so a missing pull request and a token
		// without access read alike. Naming the token is what lets somebody
		// tell them apart.
		return nil, fmt.Errorf("read %s#%d with the token in %s, which GitHub answers as though nothing is there when it cannot see the repository: %w",
			slug, number, variable, err)
	}
	t, err := anchorPullRequest(root, number, slug, pr)
	if err != nil {
		return nil, err
	}
	t.reader = reader
	return t, nil
}

// relayNouns names what each relay action hands over, for a line of output.
var relayNouns = map[string]string{
	relay.ActionRun:     "review",
	relay.ActionApprove: "approval",
}

// relayRoute is the relay a posting command on a machine holding no
// registration hands its post to, and what reaches it.
type relayRoute struct {
	// slug is the repository under review.
	slug review.Slug
	// repo is the relay repository, and from is the variable that named it.
	repo, from string
	// token reaches the relay, and tokenFrom is the variable it came from.
	token, tokenFrom string
	doer             relay.Doer
}

// routeToRelay answers a posting command that resolvePullRequest refused: with
// the relay that posts in its place, or with the error the command fails with.
//
// The relay is the answer only on a machine holding no registration at all,
// and only when relayVariable names one. A broken or half-written registration
// keeps its refusal unchanged, whose answer is to finish registering: relaying
// around it would hide the breakage the same way reading around it with a
// token would. A machine holding none and naming no relay is refused with
// both ways out.
//
// Every refusal here is made before anything is read, so a command that could
// never reach its relay spends nothing finding that out.
func routeToRelay(root string, asJSON bool, seam clientSeam, refusal error) (*relayRoute, error) {
	if !githubapp.Unregistered(refusal) {
		return nil, refusal
	}
	// resolvePullRequest read the same remote before it refused, so this
	// fails only when the remote changed in between, and the refusal then
	// stands as it was.
	slug, err := review.RemoteSlug(root, review.DefaultRemote)
	if err != nil {
		return nil, refusal
	}
	relayRepo, relayFrom := seam.relay()
	if relayRepo == "" {
		return nil, fmt.Errorf("%w, or set %s to the owner/name of a relay repository that can post to %s as the App",
			refusal, relayVariable, slug)
	}
	// A relayed command learns how the relay's run ended and not what GitHub
	// made of what it posted, so it cannot write the document --json
	// promises, and a consumer parsing this stream would be handed prose.
	if asJSON {
		return nil, fmt.Errorf("%w; %s names %s, but a relayed review reports only how the relay's run ended, "+
			"which is not the review --json describes: drop --json to relay it", refusal, relayFrom, relayRepo)
	}
	token, tokenFrom := seam.token()
	if token == "" {
		return nil, fmt.Errorf("%w; %s names %s, but neither GH_TOKEN nor GITHUB_TOKEN holds a token to reach it with",
			refusal, relayFrom, relayRepo)
	}
	return &relayRoute{
		slug: slug, repo: relayRepo, from: relayFrom,
		token: token, tokenFrom: tokenFrom,
		doer: seam.relayDoer(),
	}, nil
}

// hand dispatches req to the relay, waits for its run to end, and reports how
// it ended: nil once the run has succeeded, and otherwise the error the
// command fails with.
//
// The relay's runner makes every call that writes to the reviewed repository,
// as the App, so nothing reaches it from this process: the caller's token only
// starts the run and reads how it ended. What the run posted is on the pull
// request and in the run's log, which this process never sees, so only the
// run's conclusion and address are reported.
func (r *relayRoute) hand(ctx context.Context, env *Env, req relay.Request) error {
	noun := relayNouns[req.Action]
	target := relay.Target{Slug: r.repo, Token: r.token}
	fmt.Fprintf(env.Stdout, "This machine holds no GitHub App registration: relaying the %s of %s#%d through %s.\n",
		noun, r.slug, req.PR, r.repo)
	dispatched, err := relay.Dispatch(ctx, r.doer, target, req)
	if err != nil {
		return fmt.Errorf("relay the %s of %s#%d through %s, named by %s, with the token in %s: %w",
			noun, r.slug, req.PR, r.repo, r.from, r.tokenFrom, err)
	}
	if dispatched.URL != "" {
		fmt.Fprintf(env.Stdout, "Relay run: %s\n", dispatched.URL)
	}
	fmt.Fprintf(env.Stdout, "Waiting up to %s for it to finish.\n", relay.DefaultTimeout)
	result, err := relay.Await(ctx, r.doer, target, dispatched, 0)
	if err != nil {
		return fmt.Errorf("relay the %s of %s#%d through %s, named by %s, with the token in %s: %w",
			noun, r.slug, req.PR, r.repo, r.from, r.tokenFrom, err)
	}
	if result.Conclusion != "success" {
		return fmt.Errorf("the relay run for the %s of %s#%d did not succeed (%s): %s",
			noun, r.slug, req.PR, result.Conclusion, result.URL)
	}
	fmt.Fprintf(env.Stdout, "The relay run for the %s of %s#%d succeeded: %s\n", noun, r.slug, req.PR, result.URL)
	fmt.Fprintln(env.Stdout, "What it posted is on the pull request, and its log says how it got there.")
	return nil
}

// relayOrRefuse answers a command that resolvePullRequest refused and that
// hands the relay nothing but the pull request, and returns what the command
// reports: nil once a relay has run and succeeded, and otherwise the error the
// command fails with.
//
// It is for an action the relay's runner decides from the pull request itself,
// which approval is. routeToRelay decides whether there is a relay to hand it
// to, and hand reports how its run went.
func relayOrRefuse(ctx context.Context, env *Env, root string, number int, action, panel string, asJSON bool,
	seam clientSeam, refusal error,
) error {
	route, err := routeToRelay(root, asJSON, seam, refusal)
	if err != nil {
		return err
	}
	return route.hand(ctx, env, relay.Request{
		Repo: route.slug.String(), PR: number, Action: action, Panel: panel,
	})
}

// resolveToPost resolves a pull request for a run that posts, and returns the
// relay that posts its review when this machine cannot.
//
// A registered machine reads and posts as the App, and the route is nil. On a
// machine holding no registration at all, the relay relayVariable names posts
// in its place. Every refusal routeToRelay makes comes before anything is
// read; the pull request is then read with the token, as a run that posts
// nothing reads it, and the panel runs here. The target's client is nil, so
// nothing on this machine can post, and the route is what does.
func resolveToPost(ctx context.Context, root string, number int, flags runFlags, seam clientSeam) (*pullRequestTarget, *relayRoute, error) {
	t, err := resolvePullRequest(ctx, root, number, seam)
	if err == nil {
		return t, nil, nil
	}
	route, err := routeToRelay(root, flags.json, seam, err)
	if err != nil {
		return nil, nil, err
	}
	t, err = readPullRequest(ctx, root, number, seam)
	if err != nil {
		return nil, nil, err
	}
	return t, route, nil
}

// anchorPullRequest brings a pull request's commits into the local repository
// and works out where the change starts and where a comment may land.
//
// The head arrives by its commit id, and every ref the pipeline is then
// pointed at is that id rather than the branch name the pull request carries.
// A branch name is chosen by the change's author and can move between the read
// and the review; a review is bound to a commit, so the commit is what the
// whole run is anchored to.
func anchorPullRequest(root string, number int, slug review.Slug, pr githubapp.PullRequest) (*pullRequestTarget, error) {
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
		slug: slug, pr: pr,
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
	threads, err := t.reader.ReadReviewThreads(ctx, t.pr.Number)
	if err != nil {
		return reviewrun.ThreadsUnreadable("%v", err)
	}
	return reviewpost.ReadThreads(threads)
}

// carriesAVerdictFor reports whether this head's review reached a verdict.
//
// A review suppresses the next one only when its marker says verdict=complete.
// A run where nobody answered — every reviewer dead on an unmet schema, a judge
// that never ran — posts a review saying so, and that review is a record that
// nothing looked at the change. Reading it as "this head is reviewed" is the
// same mistake the pipeline refuses everywhere else: could not look is not
// found nothing. Here it is worse, because the marker that records the failure
// is what would prevent anyone fixing it.
//
// Which review is read is reviewapprove.LastReview's to decide, and is not
// re-implemented here. It is the newest this installation authored whose
// commit and marker head both name this one, and every clause carries: an
// older complete review must not speak for a newer forced run that failed, or
// suppression would refuse the re-review while approval refuses the head.
//
// A review carrying no marker agtk can parse counts as no verdict, which
// LastReview delivers by finding none. It is the safer reading of the two: a
// marker agtk cannot read is a review agtk cannot vouch for, and the cost of
// being wrong is one panel re-run rather than a pull request that displays a
// review nobody performed.
func carriesAVerdictFor(reviews []githubapp.SubmittedReview, head string) bool {
	marker, found := reviewapprove.LastReview(reviews, head)
	return found && marker.Complete
}

// reportUnchangedHead says that this commit already carries a review, and that
// re-deriving what the pull request already displays is what was avoided.
func reportUnchangedHead(env *Env, t *pullRequestTarget, asJSON bool) error {
	if asJSON {
		return writeJSON(env, unchangedHeadJSON(t))
	}
	fmt.Fprintf(env.Stdout, "%s#%d already carries a completed review of %s.\n", t.slug, t.pr.Number, t.pr.HeadSHA)
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
		Since:       t.scope.since,
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
//
// Each group is named by what somebody can do about it rather than by why it
// is not inline, because that is what decides whether the finding obliges an
// answer before this head is approved.
func renderPlacement(w io.Writer, place reviewpost.Placement) {
	fmt.Fprintf(w, "\n%d finding(s): %d inline, %d against a whole file, %d with nowhere to answer.\n",
		place.Total(), len(place.Inline), len(place.FileLevel), len(place.Unattachable))
	for _, f := range place.Unattachable {
		fmt.Fprintf(w, "  stated in the body — %s carries no thread to answer on, so it blocks nothing\n",
			findingLocation(f))
	}
	if deadlocked := place.Deadlocked(); len(deadlocked) > 0 {
		fmt.Fprintf(w, "  %d of those quote an instruction addressed at the reviewer. Approval is closed until the code changes.\n",
			len(deadlocked))
	}
}

// findingLocation names where a finding points, for a line of terminal output.
//
// The anchor rather than the start. A comment is attached at the end of its
// region, so that is the line GitHub validates and the line a finding is
// refused for — naming the start would report a line that is on the diff as
// the reason the finding is not on it.
func findingLocation(f reviewrun.Finding) string {
	if f.Path == "" {
		return "a finding naming no file"
	}
	if !f.HasLine() {
		return f.Path
	}
	return fmt.Sprintf("%s:%d", f.Path, reviewpost.AnchorLine(f))
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
	// A run that posts resolves as the App, and so refuses here on a machine
	// that holds no registration, before any panel is spent on a review it
	// could never post — unless a relay is named, which posts the review this
	// machine computes. A run that posts nothing reads through whatever this
	// machine has.
	var (
		t     *pullRequestTarget
		route *relayRoute
	)
	if !flags.dryRun && !flags.noPost {
		t, route, err = resolveToPost(cmd.Context(), root, target.pr, flags, seam)
	} else {
		t, err = readPullRequest(cmd.Context(), root, target.pr, seam)
	}
	if err != nil {
		return err
	}

	// Read before a reviewer is started. A head that already carries a review
	// that reached a verdict costs one query to recognise and a whole panel to
	// rediscover, and what the panel would rediscover is what the pull request
	// is already displaying. A review that reached no verdict displays nothing
	// to rediscover, so it suppresses nothing.
	reviews, reviewsErr := t.reader.ReadSubmittedReviews(cmd.Context(), t.pr.Number)
	if !flags.dryRun && !flags.force && reviewsErr == nil && carriesAVerdictFor(reviews, t.pr.HeadSHA) {
		return reportUnchangedHead(env, t, flags.json)
	}
	t.scope = chooseScope(root, t, reviews, reviewsErr, flags.full)
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
		fmt.Fprintf(env.Stdout, "scope:    %s\n", t.scope.describe())
		reviewrun.RenderPlan(env.Stdout, plan)
		renderEnvelope(env.Stdout, t, opts.Threads)
		return nil
	}

	result, err := reviewrun.Run(cmd.Context(), opts)
	if err != nil {
		return err
	}
	payload, place := reviewpost.Build(result, t.pr, t.added)
	return deliverReview(cmd.Context(), env, t, result, payload, place, route, flags)
}

// deliverReview does what a finished review of a pull request calls for:
// withholds it, posts it as the App, or hands it to a relay that posts it as
// the App from elsewhere.
//
// One function for all three, so a review that reaches a relay has been
// through every decision a review posted from here goes through, in the same
// order. route is nil when this machine holds the registration; t.client is
// nil when it does not, and then route is the only thing that can post.
func deliverReview(ctx context.Context, env *Env, t *pullRequestTarget, result *reviewrun.Review,
	payload githubapp.ReviewPayload, place reviewpost.Placement, route *relayRoute, flags runFlags,
) error {
	// A blocked review — every provider it could try declined to serve the
	// credential — takes the same path as --no-post: reported to the caller,
	// posted nowhere. A block is a credential/quota condition, not a defect
	// for a person on the pull request to see, and during an outage it would
	// otherwise repeat on every open PR. An ordinary failure is not blocked
	// and still posts, so it stays visible.
	if flags.noPost || result.Blocked {
		if err := reportReview(env, t, result, payload, place, nil, nil, flags.json); err != nil {
			return err
		}
		return unavailableError(result)
	}
	if t.client == nil {
		return relayReview(ctx, env, t, result, payload, place, route)
	}

	posted, err := t.client.CreateReview(ctx, t.pr.Number, payload)
	if err != nil {
		// The review is not lost with the post. Reporting it costs nothing and
		// is the difference between a rate limit that wasted a panel and one
		// that wasted a request. It is reported in whichever form the caller
		// asked for: a --json consumer parsing this stream must not be handed
		// prose because the post is what failed.
		if reportErr := reportReview(env, t, result, payload, place, nil, nil, flags.json); reportErr != nil {
			return reportErr
		}
		return fmt.Errorf("post the review to %s#%d: %w", t.slug, t.pr.Number, err)
	}

	// The file-level comments follow the review rather than riding inside it:
	// subject_type is not a field on a review's draft comments. They are posted
	// after the review has landed, so a failure here costs one thread and never
	// the review.
	failures := postFileComments(ctx, t.client, t.pr.Number, reviewpost.FileComments(t.pr, place))

	if err := reportReview(env, t, result, payload, place, &posted, failures, flags.json); err != nil {
		return err
	}
	reportFileComments(env, failures, flags.json)
	return unavailableError(result)
}

// relayReview hands a finished review to the relay, whose runner posts it as
// the App with `code-review post`.
//
// The relay is handed exactly the requests a registered run would make — the
// review, then each file-level comment — encoded as relayedReview, which is
// what `code-review post` decodes. The review is written out before the relay
// is dispatched, so what the panel found is on this machine's output however
// the relay's run ends.
func relayReview(ctx context.Context, env *Env, t *pullRequestTarget, result *reviewrun.Review,
	payload githubapp.ReviewPayload, place reviewpost.Placement, route *relayRoute,
) error {
	if route == nil {
		return fmt.Errorf("%s#%d was read without the App's registration and no relay was named, so its review has nowhere to be posted from",
			t.slug, t.pr.Number)
	}
	if payload.Comments == nil {
		payload.Comments = []githubapp.ReviewComment{}
	}
	raw, err := json.Marshal(relayedReview{Review: payload, FileComments: reviewpost.FileComments(t.pr, place)})
	if err != nil {
		return fmt.Errorf("encode the review of %s#%d for the relay: %w", t.slug, t.pr.Number, err)
	}

	fmt.Fprintf(env.Stdout, "scope:    %s\n", t.scope.describe())
	reviewrun.Render(env.Stdout, result)
	renderPlacement(env.Stdout, place)
	fmt.Fprintln(env.Stdout)
	if err := route.hand(ctx, env, relay.Request{
		Repo: t.slug.String(), PR: t.pr.Number, Action: relay.ActionRun, Payload: string(raw),
	}); err != nil {
		return err
	}
	return unavailableError(result)
}

// fileCommentFailure is one file-level comment GitHub would not take.
type fileCommentFailure struct {
	Path   string
	Reason string
}

// postFileComments gives every finding that hangs off a whole file a thread to
// be answered on, and reports the ones that got none.
//
// One request each, and one failure never stops the rest: they are independent
// comments rather than a batch, so the finding a rate limit swallowed is the
// only finding lost. Each is still stated in the review body, which is what
// makes a failure reportable rather than silent.
func postFileComments(ctx context.Context, client *githubapp.Client, number int, comments []githubapp.FileComment) []fileCommentFailure {
	var failures []fileCommentFailure
	for _, comment := range comments {
		if _, err := client.CreateFileComment(ctx, number, comment); err != nil {
			failures = append(failures, fileCommentFailure{Path: comment.Path, Reason: err.Error()})
		}
	}
	return failures
}

// reportFileComments says which findings never got a thread.
//
// Worth a line of its own. A finding stated in the review body with no thread
// beside it looks exactly like one agtk chose not to attach, and the two oblige
// opposite things: the second blocks nothing, and this one has to be re-posted
// before the head can be approved.
func reportFileComments(env *Env, failures []fileCommentFailure, asJSON bool) {
	if len(failures) == 0 || asJSON {
		return
	}
	fmt.Fprintf(env.Stdout, "\n%d file-level comment(s) were refused, so those findings carry no thread:\n", len(failures))
	for _, f := range failures {
		fmt.Fprintf(env.Stdout, "  %s: %s\n", f.Path, f.Reason)
	}
	fmt.Fprintln(env.Stdout, "Each is still stated in the review body. Re-run with --force to post them again.")
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
	posted *githubapp.PostedReview, failures []fileCommentFailure, asJSON bool,
) error {
	if asJSON {
		return writeJSON(env, pullRequestPostJSON(t, result, payload, place, posted, failures))
	}
	fmt.Fprintf(env.Stdout, "scope:    %s\n", t.scope.describe())
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
