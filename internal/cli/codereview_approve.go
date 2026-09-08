package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewapprove"
)

func newCodeReviewApproveCmd(env *Env) *cobra.Command {
	var (
		number int
		seam   clientSeam
	)

	cmd := &cobra.Command{
		Use:   "approve",
		Short: "Approve a pull request whose head has been reviewed",
		Long: "Posts one GitHub review with event APPROVE, as the App, bound to the pull\n" +
			"request's current head. It counts toward a required approval, which a solo\n" +
			"author cannot satisfy alone.\n" +
			"\n" +
			"Granted only when the head carries a review by this installation that reached\n" +
			"a verdict, every finding at or above the manifest's approval floor is marked a\n" +
			"false positive on its thread by an account that can push, and every comment\n" +
			"thread is resolved. Nothing overrides any of it: a finding is answered by\n" +
			"changing the code or by saying on its thread that it is not a defect.\n" +
			"\n" +
			"A person types this. No review run reaches it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodeReviewApprove(cmd, env, number, seam)
		},
	}
	cmd.Flags().IntVar(&number, "pr", 0, "pull request to approve")
	return cmd
}

// runCodeReviewApprove decides whether this head may be approved, and approves
// it when it may.
func runCodeReviewApprove(cmd *cobra.Command, env *Env, number int, seam clientSeam) error {
	if number < 1 {
		return fmt.Errorf("%d is not a pull request number", number)
	}
	root, err := review.RepoRoot(env.WorkDir)
	if err != nil {
		return fmt.Errorf("locate the repository: %w", err)
	}
	t, err := resolvePullRequest(cmd.Context(), root, number, seam)
	if err != nil {
		return err
	}
	// The floor is read from the manifest at the base ref, like every other
	// rule a pull request is judged by. A floor read from the head would let a
	// change raise the bar its own findings have to clear — ADR 0007.
	m, _, err := governingManifest(root, t.mergeBase, review.ContextPR)
	if err != nil {
		return err
	}

	outcome, err := reviewapprove.Approve(cmd.Context(), t.client, reviewapprove.Options{
		Number: t.pr.Number,
		Head:   t.pr.HeadSHA,
		Floor:  m.Approval.EffectiveFloor(),
	})
	if err != nil {
		return err
	}
	if !outcome.Approved() {
		return refusedError(env, t, outcome.Refusals)
	}
	fmt.Fprintf(env.Stdout, "Approved %s#%d at %s: %s\n", t.slug, t.pr.Number, t.pr.HeadSHA, outcome.Posted.HTMLURL)
	fmt.Fprintln(env.Stdout, "GitHub weighs this review only while the App can push, and it re-reads that when")
	fmt.Fprintln(env.Stdout, "the pull request is read rather than now — see ADR 0009.")
	return nil
}

// refusedError writes every reason the head was not approved and fails.
//
// Written to stdout and then failed, rather than folded into one error string.
// A refusal is a list of acts somebody has to perform, and a multi-line error
// prefixed with the command name reads as a malfunction rather than as work.
func refusedError(env *Env, t *pullRequestTarget, refusals []reviewapprove.Refusal) error {
	fmt.Fprintf(env.Stdout, "%s#%d was not approved at %s.\n\n", t.slug, t.pr.Number, t.pr.HeadSHA)
	for _, r := range refusals {
		fmt.Fprintf(env.Stdout, "- %s\n", r.Missing)
		if r.Remedy == "" {
			// The one refusal with no act behind it. Saying "nothing here will
			// help" is the honest report, and the deadlock is deliberate.
			fmt.Fprintln(env.Stdout, "  Nothing clears this but removing the text from the change.")
			continue
		}
		fmt.Fprintf(env.Stdout, "  %s\n", r.Remedy)
	}
	fmt.Fprintln(env.Stdout, "\nThere is no flag that approves anyway.")
	return fmt.Errorf("%d condition(s) for approving %s are not met", len(refusals), t.pr.HeadSHA)
}
