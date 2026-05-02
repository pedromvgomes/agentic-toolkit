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

	"github.com/spf13/cobra"

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
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(env.Stdin)
	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)
	root.AddCommand(newInitCmd(env), newLockCmd(env), newFetchCmd(env), newPlanCmd(env), newRenderCmd(env), newStatusCmd(env))
	return root
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
		if !errors.Is(err, errStatusDrift) {
			fmt.Fprintln(env.Stderr, "agtk:", err)
		}
		return 1
	}
	return 0
}
