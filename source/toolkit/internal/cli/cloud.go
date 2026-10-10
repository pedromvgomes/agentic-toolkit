package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/cloudinit"
)

// errCloudInit is returned by `cloud init` RunE to flip the exit code
// without a second, generic error line — the failure, written to stderr
// before this is returned, is the whole report.
var errCloudInit = errors.New("cloud init: failed")

func newCloudCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cloud",
		Short: "Prepare a cloud container session for the user's own git identity",
		// The same guard as `agtk memory`: without a RunE, cobra takes an
		// unknown subcommand as an argument, prints help and exits 0, so a
		// session-start hook naming a subcommand this binary lacks would
		// read as having succeeded.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newCloudInitCmd(env))
	return cmd
}

func newCloudInitCmd(env *Env) *cobra.Command {
	var (
		render     bool
		renderRoot string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Point git at the identity and signing key in AGTK_GH_USER, AGTK_GH_EMAIL and AGTK_SIGNING_KEY_B64",
		Long: "Writes AGTK_GH_USER and AGTK_GH_EMAIL to git's global user.name and\n" +
			"user.email, and to the current checkout when it has no identity of its\n" +
			"own. When AGTK_SIGNING_KEY_B64 is set, installs that key under ~/.ssh and\n" +
			"turns on SSH signing for commits and tags. With none of them set it\n" +
			"prints one line and changes nothing, so a session-start hook can run it\n" +
			"in every session.\n" +
			"\n" +
			"With --render, and at least one of the variables set, it then runs\n" +
			"`agtk render` in each checkout directly under --render-root that has an\n" +
			"entry manifest and a committed lockfile. It never runs `agtk sync`, so no\n" +
			"ref is resolved over the network. A checkout that fails is reported and\n" +
			"the rest still render; the command exits non-zero if any failed.",
		Args: cobra.NoArgs,
		// A session-start hook runs this in every session; the background
		// update check would make each of those starts a network call.
		Annotations: map[string]string{annotationSkipUpdateCheck: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudInit(cmd, env, render, renderRoot)
		},
	}
	cmd.Flags().BoolVar(&render, "render", false, "run 'agtk render' in each checkout under --render-root after applying the identity")
	cmd.Flags().StringVar(&renderRoot, "render-root", cloudinit.DefaultRenderRoot, "the directory whose immediate subdirectories --render considers")
	return cmd
}

func runCloudInit(cmd *cobra.Command, env *Env, render bool, renderRoot string) error {
	err := cloudinit.Run(cmd.Context(), cloudinit.Options{
		Stdout:     cmd.OutOrStdout(),
		Stderr:     env.Stderr,
		Dir:        env.WorkDir,
		Render:     render,
		RenderRoot: renderRoot,
	})
	if err == nil {
		return nil
	}
	fmt.Fprintln(env.Stderr, "agtk:", err)
	return errCloudInit
}
