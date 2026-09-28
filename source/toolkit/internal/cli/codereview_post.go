package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

// relayedReview is a review computed on one machine and posted from another.
//
// It is what `code-review post` reads on standard input, and what `run --pr`
// on a machine holding no registration hands its relay to feed it. Both sides
// encode and decode this one type, so the two cannot drift apart field by
// field. The halves are githubapp's own request bodies, sent as they arrive:
// nothing is rebuilt between the machine that ran the panel and GitHub.
type relayedReview struct {
	Review       githubapp.ReviewPayload `json:"review"`
	FileComments []githubapp.FileComment `json:"file_comments"`
}

// maxRelayedReview bounds how much of standard input is read as one review.
const maxRelayedReview = 8 << 20

func newCodeReviewPostCmd(env *Env) *cobra.Command {
	var (
		number int
		seam   clientSeam
	)

	cmd := &cobra.Command{
		Use:   "post",
		Short: "Post a review computed elsewhere to a pull request, as the App",
		Long: "Reads one review from standard input, as JSON, and posts it to the pull\n" +
			"request as the App: the review with its inline comments, then each\n" +
			"file-level comment. No panel runs and no model is started.\n" +
			"\n" +
			"It is what a relay's runner does with the review that `run --pr`, on a\n" +
			"machine holding no App registration, computed and handed it. It needs this\n" +
			"machine's own registration, and never relays.\n" +
			"\n" +
			"The input is {\"review\": {commit_id, body, event, comments}, \"file_comments\":\n" +
			"[{commit_id, path, subject_type, body}]}. It is refused when the review was\n" +
			"made against a commit that is no longer the pull request's head, and when it\n" +
			"asks for any event but COMMENT: approval is `code-review approve`'s alone.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodeReviewPost(cmd, env, number, seam)
		},
	}
	cmd.Flags().IntVar(&number, "pr", 0, "pull request to post the review to")
	return cmd
}

// runCodeReviewPost posts a review read from standard input to a pull request,
// as the App.
//
// The registration is checked before standard input is read, so a machine that
// could never post refuses the way `run --pr` and `approve` do, with nothing
// consumed. The review is read and checked before GitHub is asked anything, so
// a malformed one never reads as a network or registration failure.
func runCodeReviewPost(cmd *cobra.Command, env *Env, number int, seam clientSeam) error {
	if number < 1 {
		return fmt.Errorf("%d is not a pull request number", number)
	}
	root, err := review.RepoRoot(env.WorkDir)
	if err != nil {
		return fmt.Errorf("locate the repository: %w", err)
	}
	slug, err := review.RemoteSlug(root, review.DefaultRemote)
	if err != nil {
		return err
	}
	client, err := seam.client(slug)
	if err != nil {
		return err
	}
	in, err := readRelayedReview(cmd.InOrStdin())
	if err != nil {
		return err
	}

	// A review is bound to the commit it was made against. One made against a
	// head the branch has since moved past describes code the pull request no
	// longer holds, and the caller that made it may have finished long before
	// this runs; posting it would present a stale review as a current one.
	pr, err := client.ReadPullRequest(cmd.Context(), number)
	if err != nil {
		return err
	}
	if in.Review.CommitID != pr.HeadSHA {
		return fmt.Errorf("the review on standard input was made against %s, and the head of %s#%d is now %s: nothing was posted. "+
			"Run `agtk code-review run --pr %d` again to review the head as it is",
			in.Review.CommitID, slug, number, pr.HeadSHA, number)
	}

	posted, err := client.CreateReview(cmd.Context(), number, in.Review)
	if err != nil {
		return fmt.Errorf("post the review to %s#%d: %w", slug, number, err)
	}
	// As on a registered run: the review has landed, so a file-level comment
	// GitHub refuses costs its own thread and never the review, and is
	// reported rather than failing the command.
	failures := postFileComments(cmd.Context(), client, number, in.FileComments)
	fmt.Fprintf(env.Stdout, "Posted one review to %s#%d: %s\n", slug, number, posted.HTMLURL)
	reportFileComments(env, failures, false)
	return nil
}

// readRelayedReview reads and checks the review on standard input.
//
// Strict about its shape. A field this command does not know is one the sender
// meant to be posted, and dropping it silently would post something other than
// what was computed; a second JSON value is the same. Every error names
// standard input, so a malformed review never reads as a registration or a
// network failure.
func readRelayedReview(r io.Reader) (relayedReview, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxRelayedReview+1))
	if err != nil {
		return relayedReview{}, fmt.Errorf("read the review from standard input: %w", err)
	}
	if len(raw) > maxRelayedReview {
		return relayedReview{}, fmt.Errorf("the review on standard input is larger than %d bytes", maxRelayedReview)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return relayedReview{}, errors.New("standard input carries no review: pipe the JSON a run computed into `code-review post`")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var in relayedReview
	if err := dec.Decode(&in); err != nil {
		return relayedReview{}, fmt.Errorf("the review on standard input is not the JSON `code-review post` reads: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return relayedReview{}, errors.New("the review on standard input is followed by more than one JSON value")
	}
	return in, in.check()
}

// check refuses a review that could not have come from a run.
//
// The event above all. A run posts COMMENT and nothing else, and approval is a
// separate act behind its own gate — ADR 0006. A review asking for any other
// event would have the App approve, or request changes, on the say-so of
// whoever wrote standard input.
func (in relayedReview) check() error {
	if in.Review.CommitID == "" {
		return errors.New("the review on standard input names no commit_id, so there is no head to bind it to")
	}
	if in.Review.Event != githubapp.EventComment {
		return fmt.Errorf("the review on standard input asks for event %q, and a review is posted only as %s",
			in.Review.Event, githubapp.EventComment)
	}
	for _, c := range in.FileComments {
		if c.CommitID != in.Review.CommitID {
			return fmt.Errorf("the file-level comment on %s on standard input is bound to %s, and the review it follows to %s",
				c.Path, c.CommitID, in.Review.CommitID)
		}
		if c.Path == "" {
			return errors.New("a file-level comment on standard input names no path")
		}
	}
	return nil
}
