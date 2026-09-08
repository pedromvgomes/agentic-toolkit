package cli

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

// `explain`, `panels` and `signals` are deliberately model-free: they read a
// manifest, profile a change and decide which panel would run, and none of
// them starts a model. It is checkable — internal/review names the driver in
// one file, which asserts capabilities and constructs nothing.
//
// Model-free is not the same as offline. `explain --pr` reads the pull request
// and fetches its head, because base, head and context are what naming a pull
// request decides, and none of the three is knowable without asking GitHub. So
// bare `explain` is safe on the path of a hook and `explain --pr` is not: it
// needs the App registration, and it fails without one before a panel has run
// rather than after.
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
		newCodeReviewApproveCmd(env),
		newCodeReviewInitializeCmd(env),
		newCodeReviewExplainCmd(env),
		newCodeReviewPanelsCmd(env),
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
	// pr names an open pull request, and is zero when the target is local.
	//
	// It stands apart from base, head and context rather than beside them:
	// naming a pull request decides all three, because the change is the one
	// GitHub is holding and the context is the one that posts.
	pr int
}

// changeFlags registers the flags that name a change and the context it is
// reviewed in, so every subcommand takes the same words for the same things.
func changeFlags(cmd *cobra.Command, target *reviewTarget) {
	cmd.Flags().StringVar(&target.base, "base", "",
		"ref the change is measured against (default: the remote's default branch)")
	cmd.Flags().StringVar(&target.head, "head", "",
		"ref the change ends at (default: the working tree, uncommitted changes included)")
	cmd.Flags().StringVar(&target.context, "context", string(review.ContextWorktree),
		"what the review runs against: "+contextNames())
}

// targetFlags registers the change flags and the panel override, for the
// subcommands that decide which panel runs. `panels` lists them all and takes
// no override.
func targetFlags(cmd *cobra.Command, target *reviewTarget) {
	changeFlags(cmd, target)
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

// governingManifest reads the manifest a review in ctx is judged by, and
// returns it with the label that says which one was read.
//
// A context that posts reads its rules from the base ref. Everything on the
// branch under review is written by its author, so a manifest read from the
// working tree would let a change name the reviewers that judge it — the
// closure ADR 0007 makes structural rather than instructed. A local review of
// the working tree is the author reviewing their own change, so it reads what
// they have written.
//
// The label is returned alongside because a repo that believes it wrote a
// manifest and is being reviewed by the built-in one needs to be told: the two
// produce entirely different panels, and nothing else in any output would say
// which was read.
func governingManifest(root, mergeBase string, ctx review.Context) (*review.Manifest, string, error) {
	var (
		m       *review.Manifest
		path    string
		builtin bool
		err     error
	)
	if ctx.Posts() {
		m, path, builtin, err = review.LoadAtRef(root, mergeBase)
	} else {
		m, path, builtin, err = review.Load(root)
	}
	if err != nil {
		return nil, "", err
	}
	label := manifestLabel(path, builtin)
	if err := review.CheckCapabilities(label, m); err != nil {
		return nil, "", err
	}
	return m, label, nil
}

func newCodeReviewExplainCmd(env *Env) *cobra.Command {
	var (
		target reviewTarget
		asJSON bool
		seam   clientSeam
	)

	cmd := &cobra.Command{
		Use:   "explain",
		Short: "Report which panel a review would run, and why",
		Long: "Prints the change's profile, the panel the context defaults to, every\n" +
			"escalation rule that fired, and the panel that resulted.\n" +
			"\n" +
			"Nothing is spent: no model runs and nothing is posted. It is the answer to\n" +
			"\"why is this review deeper than I expected\", available before paying for\n" +
			"the review that would tell you.\n" +
			"\n" +
			"--pr answers it for an open pull request, under the rules its base ref\n" +
			"declares. That reads the pull request and fetches its head, so it needs the\n" +
			"App registration `code-review initialize` writes; without --pr nothing is\n" +
			"read but this repository.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodeReviewExplain(cmd, env, target, asJSON, seam)
		},
	}
	targetFlags(cmd, &target)
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the decision as JSON")
	cmd.Flags().IntVar(&target.pr, "pr", 0,
		"explain the review this open pull request would get")
	return cmd
}

func runCodeReviewExplain(cmd *cobra.Command, env *Env, target reviewTarget, asJSON bool, seam clientSeam) error {
	// Named rather than non-zero: `--pr 0` is a pull request nobody has, and
	// routing it here by its value would explain the working tree instead —
	// silently, and accepting the --base and --head that the pull-request path
	// refuses.
	if namedPullRequest(cmd, target) {
		return explainPullRequest(cmd, env, target, asJSON, seam)
	}
	ctx := review.Context(target.context)
	root, base, mergeBase, err := resolveTarget(env, target)
	if err != nil {
		return err
	}

	m, label, err := governingManifest(root, mergeBase, ctx)
	if err != nil {
		return err
	}

	profile, err := review.BuildProfile(review.ProfileOptions{
		Dir:     root,
		Base:    mergeBase,
		Head:    target.head,
		Exclude: m.Exclude,
	})
	if err != nil {
		return err
	}

	sel, err := review.Select(m, ctx, profile, target.panel)
	if err != nil {
		return err
	}

	return writeExplain(env, asJSON, label, rangeLabel(base, target.head), m, profile, sel)
}

// explainPullRequest reports the panel an open pull request would get.
//
// It resolves the pull request exactly as `run --pr` does rather than deriving
// the range from flags, because the point of the subcommand is to answer for
// the review that would actually happen. A base and head worked out any other
// way would explain a different change, and would do it convincingly.
func explainPullRequest(cmd *cobra.Command, env *Env, target reviewTarget, asJSON bool, seam clientSeam) error {
	if err := checkPullRequestFlags(target, cmd.Flags().Changed("context")); err != nil {
		return err
	}
	root, err := review.RepoRoot(env.WorkDir)
	if err != nil {
		return fmt.Errorf("locate the repository: %w", err)
	}
	t, err := resolvePullRequest(cmd.Context(), root, target.pr, seam)
	if err != nil {
		return err
	}

	m, label, err := governingManifest(root, t.mergeBase, review.ContextPR)
	if err != nil {
		return err
	}

	profile, err := review.BuildProfile(review.ProfileOptions{
		Dir:     root,
		Base:    t.mergeBase,
		Head:    t.pr.HeadSHA,
		Exclude: m.Exclude,
	})
	if err != nil {
		return err
	}

	sel, err := review.Select(m, review.ContextPR, profile, target.panel)
	if err != nil {
		return err
	}

	return writeExplain(env, asJSON, label, rangeLabel(t.pr.BaseRef, t.pr.HeadSHA), m, profile, sel)
}

// writeExplain reports a selection in whichever form the caller asked for, so
// the two targets answer in one shape.
func writeExplain(env *Env, asJSON bool, label, rng string, m *review.Manifest, profile *review.Profile, sel *review.Selection) error {
	if asJSON {
		return writeJSON(env, explainJSON(label, rng, m, profile, sel))
	}
	fmt.Fprintf(env.Stdout, "manifest: %s\n", label)
	fmt.Fprintf(env.Stdout, "range:    %s\n", rng)
	fmt.Fprint(env.Stdout, sel.Explain(m, profile))
	return nil
}

func newCodeReviewPanelsCmd(env *Env) *cobra.Command {
	var (
		target reviewTarget
		asJSON bool
	)

	cmd := &cobra.Command{
		Use:   "panels",
		Short: "List the panels the governing manifest declares",
		Long: "Prints every panel the manifest declares, with what each is for and what it\n" +
			"costs. The names are the ones --panel accepts.\n" +
			"\n" +
			"Panel names belong to the manifest: the built-in default declares quick,\n" +
			"standard and deep, and a repo that wrote its own may call them anything. A\n" +
			"caller offering a choice of depth reads the names here rather than knowing\n" +
			"them.\n" +
			"\n" +
			"The manifest listed is the one a review in --context would be judged by, so\n" +
			"a PR context lists the panels declared at the base ref.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodeReviewPanels(env, target, asJSON)
		},
	}
	changeFlags(cmd, &target)
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the list as JSON")
	return cmd
}

func runCodeReviewPanels(env *Env, target reviewTarget, asJSON bool) error {
	ctx := review.Context(target.context)
	root, _, mergeBase, err := resolveTarget(env, target)
	if err != nil {
		return err
	}

	m, label, err := governingManifest(root, mergeBase, ctx)
	if err != nil {
		return err
	}

	if asJSON {
		return writeJSON(env, panelsJSON(label, ctx, m))
	}
	fmt.Fprintf(env.Stdout, "manifest: %s\n", label)
	for _, name := range panelsByDepth(m) {
		fmt.Fprintf(env.Stdout, "\n%s%s", name, review.PanelShape(m, name))
		if contexts := defaultFor(m, name); len(contexts) > 0 {
			fmt.Fprintf(env.Stdout, " — default for %s", strings.Join(contexts, ", "))
		}
		fmt.Fprintln(env.Stdout)
		if desc := m.Panels[name].Description; desc != "" {
			fmt.Fprintf(env.Stdout, "    %s\n", desc)
		}
	}
	return nil
}

// panelsByDepth lists a manifest's panels shallowest first, by name among
// equals. Cost is the order escalations already use, so a listing in that
// order reads as the ladder a rule climbs; declaration order in a YAML map
// carries no meaning.
func panelsByDepth(m *review.Manifest) []string {
	names := make([]string, 0, len(m.Panels))
	for name := range m.Panels {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		ci, cj := m.Panels[names[i]].Cost(), m.Panels[names[j]].Cost()
		if ci != cj {
			return ci < cj
		}
		return names[i] < names[j]
	})
	return names
}

// defaultFor names the contexts that start from this panel, in the order help
// lists them.
func defaultFor(m *review.Manifest, name string) []string {
	out := []string{}
	for _, ctx := range review.Contexts {
		if m.Defaults.Default(ctx) == name {
			out = append(out, string(ctx))
		}
	}
	return out
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

// namedPullRequest reports whether the caller pointed this command at a pull
// request, by flag or by a target built in code.
func namedPullRequest(cmd *cobra.Command, target reviewTarget) bool {
	return target.pr != 0 || cmd.Flags().Changed("pr")
}
