package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

func newCodeReviewInitCmd(env *Env) *cobra.Command {
	var (
		force  bool
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write this repo's own review manifest, starting from the built-in default",
		Long: "Writes " + review.ManifestRelPath + " holding the manifest agtk already\n" +
			"reviews this repo by.\n" +
			"\n" +
			"A manifest is not required: a repo without one is reviewed by the default\n" +
			"that ships in the binary, which is what makes code-review work on a repo\n" +
			"that has just adopted the toolkit. This is for the point where you want to\n" +
			"change it — a different provider, another panel, a rule of your own.\n" +
			"\n" +
			"What it writes is the default verbatim. One thing does change with it:\n" +
			"a rule the built-in default cannot evaluate is skipped, because a repo\n" +
			"with no manifest has nothing to edit and no way to take the advice a\n" +
			"refusal gives. The same rule in your manifest is a refusal, since now you\n" +
			"can. Any rule that would do that here is named after writing.\n" +
			"\n" +
			"`code-review explain` says which manifest is governing, before and after.\n" +
			"\n" +
			"Refuses to overwrite an existing manifest unless --force is given.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodeReviewInit(env, force, dryRun)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing manifest")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report where the manifest would be written and write nothing")
	return cmd
}

func runCodeReviewInit(env *Env, force, dryRun bool) error {
	root, err := review.RepoRoot(env.WorkDir)
	if err != nil {
		return fmt.Errorf("locate the repository: %w", err)
	}
	path := review.ManifestPath(root)
	legacy := review.LegacyManifestPath(root)
	_, legacyErr := os.Stat(legacy)
	hasLegacy := legacyErr == nil

	// Reported before the existence check, because saying where the manifest
	// lives is the one thing --dry-run is for, and it is most useful in the
	// repo that already has one.
	if dryRun {
		fmt.Fprintf(env.Stdout, "manifest: %s\n", path)
		if _, err := os.Stat(path); err == nil {
			fmt.Fprintln(env.Stdout, "It exists already; writing over it needs --force.")
		}
		if hasLegacy {
			fmt.Fprintf(env.Stdout, "%s holds a manifest agtk no longer reads; %s moves it.\n",
				legacy, legacyMoveCommand())
		}
		fmt.Fprintln(env.Stdout, "\nNothing was written.")
		return nil
	}

	// A manifest at the path ManifestDir replaced is a repo mid-migration, and
	// writing here would end the migration in the worst possible way: the new
	// path exists, so every loader stops looking at the old one and finds the
	// built-in default instead of refusing. The repo's panels, judge and
	// approval floor are gone with nothing saying so — which is what the
	// refusal in `review.Load` exists to prevent, reached through the command
	// somebody is most likely to run while moving the file.
	if hasLegacy && !force {
		return fmt.Errorf("%s holds this repo's review rules, at a path agtk no longer reads; %s keeps them, and --force writes the default over them instead",
			legacy, legacyMoveCommand())
	} else if legacyErr != nil && !os.IsNotExist(legacyErr) {
		return fmt.Errorf("read %s: %w", legacy, legacyErr)
	}

	// Checked before writing, because a manifest is the file a repo tuned its
	// own reviews in: overwriting one with the default would replace every rule
	// it had with none of them, and the reviews would go on running as though
	// that were what somebody wanted.
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists; --force overwrites it, and it is this repo's own review rules", path)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301 -- 0755: the review manifest's directory in the user's repo
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, review.DefaultManifestYAML(), 0o644); err != nil { // #nosec G306 -- 0644: the review manifest in the user's repo, meant to be committed
		return fmt.Errorf("write %s: %w", path, err)
	}

	fmt.Fprintf(env.Stdout, "Wrote %s.\n", path)
	fmt.Fprintln(env.Stdout, "It is the built-in default verbatim.")
	fmt.Fprintln(env.Stdout, "`agtk code-review panels` lists what it declares; `explain` says which one governs.")
	reportUnrunnableRules(env, root)
	return nil
}

// reportUnrunnableRules names a rule this repo's new manifest cannot evaluate.
//
// The same bytes mean something different once a repo owns them. A rule the
// built-in default cannot evaluate is skipped, because a repo with no manifest
// has nothing to edit and cannot take the advice a refusal gives; the identical
// rule in the repo's own manifest is a refusal, because now it can. Writing the
// default is therefore the one moment that turns a skipped rule into a review
// that will not run, and saying so here is the difference between a remedy and
// a mystery at the next review.
//
// Best-effort by construction: a repo with no base ref to profile against has
// nothing to check, and failing to check is never a reason to fail the write.
func reportUnrunnableRules(env *Env, root string) {
	m, _, _, err := review.Load(root)
	if err != nil {
		return
	}
	base, err := review.DetectBase(root)
	if err != nil {
		return
	}
	mergeBase, err := review.MergeBase(root, base, "")
	if err != nil {
		return
	}
	profile, err := review.BuildProfile(review.ProfileOptions{Dir: root, Base: mergeBase, Exclude: m.Exclude})
	if err != nil {
		return
	}
	for _, ctx := range review.Contexts {
		if _, err := review.Select(m, ctx, profile, ""); err != nil {
			fmt.Fprintf(env.Stderr, "\nA rule in it cannot be evaluated in this repository:\n  %v\n", err)
			fmt.Fprintln(env.Stderr, "The built-in default skipped that rule; your own manifest refuses on it, so edit or")
			fmt.Fprintln(env.Stderr, "remove it before the next review.")
			return
		}
	}
}

// legacyMoveCommand is the git invocation that moves a manifest to where agtk
// reads it, named in both the refusal and the dry run so the remedy reads the
// same either way.
func legacyMoveCommand() string {
	return "`git mv " + review.LegacyManifestDir + " " + review.ManifestDir + "`"
}
