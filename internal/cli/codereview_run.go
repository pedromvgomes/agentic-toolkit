package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun"
)

// runFlags are the knobs that bound a review rather than name its target.
type runFlags struct {
	timeout     time.Duration
	maxParallel int
	dryRun      bool
	noPost      bool
	force       bool
	json        bool
}

func newCodeReviewRunCmd(env *Env) *cobra.Command {
	var (
		target reviewTarget
		flags  runFlags
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the panel over a change and report what survives",
		Long: "Assembles the diff, the repo's convention documents and a detached copy of\n" +
			"the code, runs the panel's reviewers in parallel, puts what they find to a\n" +
			"validator and then to the judge, and prints what survives.\n" +
			"\n" +
			"Without --pr nothing is posted at all. The judge decides what the review\n" +
			"says and agtk transmits it, which is why no model in the run holds a GitHub\n" +
			"credential.\n" +
			"\n" +
			"--pr reviews an open pull request and posts what survives to it as one\n" +
			"review, with event COMMENT. Nothing else is ever posted: approval is a\n" +
			"separate act with its own subcommand, and no code path from here reaches it.\n" +
			"\n" +
			"--dry-run prints the assembled prompts and the runs that would be made, and\n" +
			"spends nothing. --no-post runs the panel for real and prints the exact\n" +
			"request it would have made instead of making it.\n" +
			"\n" +
			"A head that already carries a review of the same commit is a no-op that says\n" +
			"so and spends nothing; --force reviews it again. Findings the pull request\n" +
			"already carries are withheld either way, so re-running after a push posts\n" +
			"what is new and nothing else.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodeReviewRun(cmd, env, target, flags)
		},
	}
	targetFlags(cmd, &target)
	cmd.Flags().DurationVar(&flags.timeout, "timeout", reviewrun.DefaultTimeout,
		"how long one run may take")
	cmd.Flags().IntVar(&flags.maxParallel, "max-parallel", reviewrun.DefaultParallel,
		"how many runs may be in flight at once; a provider that reports its own limit is held to the lower of the two")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false,
		"print the assembled prompts and the runs that would be made, and spend nothing")
	cmd.Flags().BoolVar(&flags.noPost, "no-post", false,
		"run the panel and print the request that would post the review, without posting it")
	cmd.Flags().BoolVar(&flags.force, "force", false,
		"review a head that already carries a review, instead of stopping")
	cmd.Flags().BoolVar(&flags.json, "json", false, "emit the review as JSON")
	cmd.Flags().IntVar(&target.pr, "pr", 0,
		"review this open pull request and post the result to it")
	return cmd
}

func runCodeReviewRun(cmd *cobra.Command, env *Env, target reviewTarget, flags runFlags) error {
	if namedPullRequest(cmd, target) {
		return runCodeReviewPR(cmd, env, target, flags, clientSeam{})
	}
	// --no-post withholds the post a --pr review would make. Without --pr
	// there is none, and a flag that is silently ignored reads as a flag that
	// was honoured — which on this one means believing a review was withheld.
	if flags.noPost {
		return errors.New("--no-post withholds the review a --pr run would post; without --pr there is nothing to withhold")
	}
	// --force overrides the check that a head already carries a review, and
	// only a pull request carries one. Silently ignoring it would read as a
	// flag that was honoured.
	if flags.force {
		return errors.New("--force reviews a pull request head that already carries a review; without --pr there is no posted review to override")
	}
	root, base, mergeBase, err := resolveTarget(env, target)
	if err != nil {
		return err
	}

	opts := reviewrun.Options{
		Dir:         root,
		Base:        mergeBase,
		BaseLabel:   base,
		Head:        target.head,
		Context:     review.Context(target.context),
		Panel:       target.panel,
		Timeout:     flags.timeout,
		MaxParallel: flags.maxParallel,
	}

	if flags.dryRun {
		opts.Preview = true
		plan, _, _, reviewRoot, err := reviewrun.Prepare(opts)
		if err != nil {
			return err
		}
		defer func() { _ = reviewRoot.Close() }()
		if flags.json {
			return writeJSON(env, planRunJSON(plan))
		}
		reviewrun.RenderPlan(env.Stdout, plan)
		return nil
	}

	result, err := reviewrun.Run(cmd.Context(), opts)
	if err != nil {
		return err
	}

	if flags.json {
		if err := writeJSON(env, reviewJSON(result)); err != nil {
			return err
		}
	} else {
		reviewrun.Render(env.Stdout, result)
	}

	// A review that could not reach a verdict exits non-zero. Findings never
	// do: what a review found is the review's content, and a severity floor
	// that blocks is a property of approval rather than of running a panel.
	if !result.Available {
		return fmt.Errorf("the review could not reach a verdict: %s", result.Reason)
	}
	return nil
}
