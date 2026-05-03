// Package cli wires the cobra-based command tree for the agtk binary.
// Subcommand impls live alongside this file; cmd/agtk/main.go simply
// constructs Env from os.Stdin/Stdout/Stderr and calls Execute.
//
// Slice-2 commands: init, lock, fetch, plan. Each subcommand reads from
// and writes to Env rather than the global os.* streams so package
// tests can capture output and use a temp working directory.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/pedromvgomes/agentic-toolkit/internal/updatecheck"
	"github.com/pedromvgomes/agentic-toolkit/internal/updater"
	"github.com/pedromvgomes/agentic-toolkit/internal/updatestate"
	"github.com/pedromvgomes/agentic-toolkit/internal/userconfig"
	"github.com/pedromvgomes/agentic-toolkit/internal/version"
)

// Env carries per-invocation streams and the working directory the
// commands resolve config/lockfile paths against. Tests construct an
// Env explicitly; Execute fills it from os.* defaults.
type Env struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	WorkDir string

	// UpdateProvider, when non-nil, replaces the default GitHub-backed
	// LatestVersionProvider. Tests inject stubs to keep the network
	// out of unit tests.
	UpdateProvider updatecheck.LatestVersionProvider

	// UpdateInstaller, when non-nil, replaces the default GitHub-backed
	// installer. Tests inject stubs to assert on Install calls without
	// touching the running binary.
	UpdateInstaller updater.Installer

	// UpdateResult, when non-nil, is the channel the persistent
	// pre-run hook posts background-check results to. The post-run
	// hook drains it non-blockingly to surface a one-liner.
	UpdateResult <-chan updatecheck.UpdateInfo
}

// ConfigFileName is the canonical filename for the consumer config in
// the working directory. The filename is fixed in slice 2 — no global
// override flag — to keep discovery predictable.
const ConfigFileName = ".agentic-toolkit.yaml"

// LockFileName is the canonical filename for the lockfile in the
// working directory.
const LockFileName = ".agentic-toolkit.lock.yaml"

// NewRootCmd builds the root cobra command and attaches the subcommand
// tree. Each subcommand captures env by closure so it can read/write
// from the right streams and resolve relative paths against env.WorkDir.
func NewRootCmd(env *Env) *cobra.Command {
	root := &cobra.Command{
		Use:           "agtk",
		Short:         "agentic toolkit",
		Long:          "agtk drives the agentic-toolkit consumer workflow: init a config, lock its sources, fetch them into a cache, and inspect the resolved plan.",
		Version:       version.Current(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(env.Stdin)
	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)

	// Persistent pre-run: spawn the background update checker once for
	// the whole invocation. The result channel is drained in
	// PersistentPostRunE so a slow network never delays exit.
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if env.UpdateResult == nil && cmd.Name() != "update" {
			env.UpdateResult = startBackgroundCheck(env)
		}
		return nil
	}
	root.PersistentPostRunE = func(cmd *cobra.Command, args []string) error {
		drainBackgroundCheck(env)
		return nil
	}

	root.AddCommand(
		newInitCmd(env), newLockCmd(env), newFetchCmd(env), newPlanCmd(env),
		newRenderCmd(env), newSyncCmd(env), newStatusCmd(env), newUpdateCmd(env),
	)
	return root
}

// startBackgroundCheck applies the throttle gates and, if all pass,
// spawns one Checker goroutine. Returns the result channel; nil when
// any gate is closed.
func startBackgroundCheck(env *Env) <-chan updatecheck.UpdateInfo {
	current := version.Current()
	if current == "dev" {
		return nil
	}
	cfg, err := userconfig.Load()
	if err != nil {
		// Misconfigured user file: don't crash, just skip.
		return nil
	}
	state, _ := updatestate.Load()
	gate := updatecheck.Gate{
		Now:            time.Now(),
		IsTerminal:     stdoutIsTerminal(env),
		CurrentVersion: current,
		Config:         cfg.AutoUpdate,
		State:          state,
	}
	if !updatecheck.ShouldCheck(gate) {
		return nil
	}
	provider := env.UpdateProvider
	if provider == nil {
		provider = updatecheck.NewGitHubProvider(githubOwner, githubRepo)
	}
	checker := updatecheck.NewChecker(provider, current)
	statePath, _ := updatestate.Path()
	checker.Start(statePath)
	return checker.Result
}

// drainBackgroundCheck does a non-blocking select on env.UpdateResult.
// If a positive UpdateInfo is already there, prints a one-liner to
// stderr; otherwise drops the result silently.
func drainBackgroundCheck(env *Env) {
	if env.UpdateResult == nil {
		return
	}
	select {
	case info, ok := <-env.UpdateResult:
		if !ok || !info.Available {
			return
		}
		fmt.Fprintf(env.Stderr, "agtk %s is available (you're on %s). Run 'agtk update' to install.\n",
			info.Latest, info.Current)
	default:
		return
	}
}

// stdoutIsTerminal returns true when env.Stdout is the process's real
// stdout AND that stdout is a terminal. Tests pass buffers and get
// false here, which is what we want.
func stdoutIsTerminal(env *Env) bool {
	if env.Stdout != os.Stdout {
		return false
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// Execute runs the CLI with the process's standard streams and cwd.
// Returns the exit code main should pass to os.Exit.
func Execute() int {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agtk:", err)
		return 1
	}
	env := &Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, WorkDir: wd}
	if err := NewRootCmd(env).Execute(); err != nil {
		// `agtk status` prints its own structured drift report and
		// returns errStatusDrift to flip the exit code; suppress the
		// generic error prefix in that case so users see only the
		// bucket lines.
		if errors.Is(err, errStatusDrift) {
			return 1
		}
		// `agtk update --check` returns updateNewerErr when newer is
		// available; map that to UpdateCheckExitCode without the
		// generic prefix so scripts can read the message verbatim.
		var newerErr *updateNewerErr
		if errors.As(err, &newerErr) {
			fmt.Fprintln(env.Stdout, newerErr.Error())
			return UpdateCheckExitCode
		}
		fmt.Fprintln(env.Stderr, "agtk:", err)
		return 1
	}
	return 0
}
