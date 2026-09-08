package cli

import (
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
			"Nothing is posted. The judge decides what the review says and agtk transmits\n" +
			"it, and transmitting is a separate command.\n" +
			"\n" +
			"--dry-run prints the assembled prompts and the runs that would be made, and\n" +
			"spends nothing.",
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
	cmd.Flags().BoolVar(&flags.json, "json", false, "emit the review as JSON")
	return cmd
}

func runCodeReviewRun(cmd *cobra.Command, env *Env, target reviewTarget, flags runFlags) error {
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
