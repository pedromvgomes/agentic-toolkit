package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

// `explain` and `signals` are deliberately model-free: they read a manifest,
// profile a change and decide which panel would run, and neither starts a
// process. That is what makes `explain` free to run on a hook, and it is
// checkable — internal/review names the driver in one file, which asserts
// capabilities and constructs nothing.
//
// `run` is the one subcommand that invokes a model, and it reaches one through
// internal/reviewrun rather than by constructing a driver here.
func newCodeReviewCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "code-review",
		Short: "Review a change with a panel of reviewers",
		Long: "Reads " + review.ManifestDir + "/" + review.ManifestFile + ", profiles the change, and\n" +
			"decides which panel would run against it.\n" +
			"\n" +
			"A repo with no manifest is reviewed by the one built into agtk. A repo with\n" +
			"one is using it whole: prompt bodies stay shareable through `builtin:`\n" +
			"references rather than through a merge.",
		// Cobra rejects an unknown subcommand only at the root: a non-root
		// parent takes it as an argument, prints help and exits 0. An agent
		// told to run a subcommand this binary does not have would read that
		// help as the command's output and report success. NoArgs alone does
		// not close it, because a command with no Run is not Runnable and
		// cobra returns ErrHelp before validating args.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(
		newCodeReviewRunCmd(env),
		newCodeReviewExplainCmd(env),
		newCodeReviewSignalsCmd(env),
	)
	return cmd
}

// reviewTarget is what a code-review subcommand was pointed at.
type reviewTarget struct {
	base    string
	head    string
	context string
	panel   string
}

// targetFlags registers the flags that name a change, so `explain` and `run`
// take the same words for the same things.
func targetFlags(cmd *cobra.Command, target *reviewTarget) {
	cmd.Flags().StringVar(&target.base, "base", "",
		"ref the change is measured against (default: the remote's default branch)")
	cmd.Flags().StringVar(&target.head, "head", "",
		"ref the change ends at (default: the working tree, uncommitted changes included)")
	cmd.Flags().StringVar(&target.context, "context", string(review.ContextWorktree),
		"what the review runs against: "+contextNames())
	cmd.Flags().StringVar(&target.panel, "panel", "",
		"run this panel instead of the one the rules choose")
}

// resolveTarget turns a target into the repository root and the merge base the
// change is anchored to.
// base is the ref the caller named or the one detected; mergeBase is where it
// and the head diverged, which is what the change is actually measured from.
// Both are returned because the report names the first and the work uses the
// second.
func resolveTarget(env *Env, target reviewTarget) (root, base, mergeBase string, err error) {
	if !knownContext(review.Context(target.context)) {
		return "", "", "", fmt.Errorf("%q is not a context; use one of %s", target.context, contextNames())
	}
	root, err = review.RepoRoot(env.WorkDir)
	if err != nil {
		return "", "", "", fmt.Errorf("locate the repository: %w", err)
	}
	base = target.base
	if base == "" {
		if base, err = review.DetectBase(root); err != nil {
			return "", "", "", err
		}
	}
	mergeBase, err = review.MergeBase(root, base, target.head)
	if err != nil {
		return "", "", "", fmt.Errorf("anchor the change to %s: %w", base, err)
	}
	return root, base, mergeBase, nil
}

func newCodeReviewExplainCmd(env *Env) *cobra.Command {
	var target reviewTarget

	cmd := &cobra.Command{
		Use:   "explain",
		Short: "Report which panel a review would run, and why",
		Long: "Prints the change's profile, the panel the context defaults to, every\n" +
			"escalation rule that fired, and the panel that resulted.\n" +
			"\n" +
			"Nothing is spent: no model runs and nothing is posted. It is the answer to\n" +
			"\"why is this review deeper than I expected\", available before paying for\n" +
			"the review that would tell you.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodeReviewExplain(env, target)
		},
	}
	targetFlags(cmd, &target)
	return cmd
}

func runCodeReviewExplain(env *Env, target reviewTarget) error {
	ctx := review.Context(target.context)
	root, base, mergeBase, err := resolveTarget(env, target)
	if err != nil {
		return err
	}

	// A context that posts reads its rules from the base ref. Everything on
	// the branch under review is written by its author, so a manifest read
	// from the working tree would let a change name the reviewers that judge
	// it — the closure ADR 0007 makes structural rather than instructed.
	var (
		m       *review.Manifest
		path    string
		builtin bool
	)
	if ctx.Posts() {
		m, path, builtin, err = review.LoadAtRef(root, mergeBase)
	} else {
		m, path, builtin, err = review.Load(root)
	}
	if err != nil {
		return err
	}
	if err := review.CheckCapabilities(manifestLabel(path, builtin), m); err != nil {
		return err
	}

	profile, err := review.BuildProfile(review.ProfileOptions{
		Dir:  root,
		Base: mergeBase,
		Head: target.head,
	})
	if err != nil {
		return err
	}

	sel, err := review.Select(m, ctx, profile, target.panel)
	if err != nil {
		return err
	}

	// A repo that believes it wrote a manifest and is being reviewed by the
	// built-in one needs to be told: the two produce entirely different
	// panels, and nothing else in this output would say which was read.
	fmt.Fprintf(env.Stdout, "manifest: %s\n", manifestLabel(path, builtin))
	fmt.Fprintf(env.Stdout, "range:    %s\n", rangeLabel(base, target.head))
	fmt.Fprint(env.Stdout, sel.Explain(m, profile))
	return nil
}

func newCodeReviewSignalsCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "signals",
		Short: "List the signals a change can carry",
		Long: "The vocabulary is closed and ships with the binary. Detecting a signal is\n" +
			"language knowledge, and language knowledge has to be tested somewhere other\n" +
			"than a consumer's YAML — a repo that wrote its own patterns gets nothing the\n" +
			"day it adds a second language. A repo's own escape hatch is `touches`, which\n" +
			"is honest about being path-only.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
			for _, sig := range review.Signals {
				fmt.Fprintf(w, "%s\t%s\n", sig, sig.Description())
			}
			return w.Flush()
		},
	}
}

// manifestLabel names which manifest was read.
func manifestLabel(path string, builtin bool) string {
	if builtin {
		return "built-in default (this repo declares no " + review.ManifestDir + "/" + review.ManifestFile + ")"
	}
	return path
}

// rangeLabel renders what the change was measured over.
func rangeLabel(base, head string) string {
	if head == "" {
		return base + "...working tree"
	}
	return base + "..." + head
}

func contextNames() string {
	names := make([]string, 0, len(review.Contexts))
	for _, c := range review.Contexts {
		names = append(names, string(c))
	}
	return strings.Join(names, ", ")
}

func knownContext(c review.Context) bool {
	for _, known := range review.Contexts {
		if known == c {
			return true
		}
	}
	return false
}
