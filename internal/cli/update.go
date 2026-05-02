package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/updatecheck"
	"github.com/pedromvgomes/agentic-toolkit/internal/updater"
	"github.com/pedromvgomes/agentic-toolkit/internal/version"
)

// UpdateCheckExitCode is the conventional non-zero status returned by
// `agtk update --check` when a newer version is available. Scriptable
// CI gates can branch on it.
const UpdateCheckExitCode = 10

// errUpdateNewer is the sentinel returned by --check on a positive
// availability hit. Execute() maps it to UpdateCheckExitCode and
// suppresses the generic `agtk:` error prefix.
type updateNewerErr struct{ latest, current string }

func (e *updateNewerErr) Error() string {
	return fmt.Sprintf("update available: %s (running %s)", e.latest, e.current)
}

func newUpdateCmd(env *Env) *cobra.Command {
	var (
		check bool
		yes   bool
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for and install the latest agtk release",
		Long: "Queries the GitHub releases API for the latest agtk tag and (unless\n" +
			"--check is passed) replaces the running binary in place.\n" +
			"\n" +
			"Modes:\n" +
			"  agtk update           Prompt before installing.\n" +
			"  agtk update --yes     Install immediately if newer.\n" +
			"  agtk update --check   Print the latest version. Exit 0 if up to date,\n" +
			"                        " + fmt.Sprintf("%d", UpdateCheckExitCode) + " if a newer release is available. No install.\n" +
			"\n" +
			"Dev builds (Version=\"dev\") report 'no version known' and exit 0 from\n" +
			"--check; install is refused.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(env, check, yes)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "report newer version availability without installing (exit 10 if available)")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the install confirmation prompt")
	return cmd
}

func runUpdate(env *Env, checkOnly, yes bool) error {
	current := version.Current()
	if version.IsDev() {
		fmt.Fprintln(env.Stdout, "running a dev build (no version tag); update unavailable")
		return nil
	}

	provider := env.UpdateProvider
	if provider == nil {
		provider = updatecheck.NewGitHubProvider(githubOwner, githubRepo)
	}

	checker := updatecheck.NewChecker(provider, current)
	checker.Start("") // no state persistence — `agtk update` is explicit
	info, ok := <-checker.Result
	if !ok {
		return fmt.Errorf("update check failed (no response)")
	}
	fmt.Fprintf(env.Stdout, "current: %s\nlatest:  %s\n", info.Current, info.Latest)
	if !info.Available {
		fmt.Fprintln(env.Stdout, "up to date")
		return nil
	}

	if checkOnly {
		return &updateNewerErr{latest: info.Latest, current: info.Current}
	}

	if !yes {
		fmt.Fprintf(env.Stdout, "install %s? [y/N] ", info.Latest)
		reader := bufio.NewReader(env.Stdin)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))
		if line != "y" && line != "yes" {
			fmt.Fprintln(env.Stdout, "aborted")
			return nil
		}
	}

	installer := env.UpdateInstaller
	if installer == nil {
		installer = updater.NewGitHubInstaller(githubOwner, githubRepo)
	}
	if err := updater.CheckExecutableWritable(); err != nil {
		return err
	}
	if err := installer.Install(info.Latest); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "installed %s\n", info.Latest)
	return nil
}

// githubOwner / githubRepo identify the canonical agtk repository for
// release downloads. Centralized here so tests can swap by injecting a
// stub provider on Env instead.
const (
	githubOwner = "pedromvgomes"
	githubRepo  = "agentic-toolkit"
)
