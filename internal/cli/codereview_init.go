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
			"What it writes is the default verbatim, so nothing about your reviews\n" +
			"changes until you edit it. `code-review explain` says which manifest is\n" +
			"governing, before and after.\n" +
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

	// Checked before writing rather than after, because a manifest is the file
	// a repo tuned its own reviews in: overwriting one with the default would
	// replace every rule it had with none of them, and the reviews would go on
	// running as though that were what somebody wanted.
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists; --force overwrites it, and it is this repo's own review rules", path)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	if dryRun {
		fmt.Fprintf(env.Stdout, "manifest: %s\n", path)
		fmt.Fprintln(env.Stdout, "\nNothing was written.")
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301 -- 0755: the review manifest's directory in the user's repo
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, review.DefaultManifestYAML(), 0o644); err != nil { // #nosec G306 -- 0644: the review manifest in the user's repo, meant to be committed
		return fmt.Errorf("write %s: %w", path, err)
	}

	fmt.Fprintf(env.Stdout, "Wrote %s.\n", path)
	fmt.Fprintln(env.Stdout, "It is the built-in default verbatim, so no review changes until you edit it.")
	fmt.Fprintln(env.Stdout, "`agtk code-review panels` lists what it declares; `explain` says which one governs.")
	return nil
}
