package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/guard"
)

// errGuardFootersDenied is returned by `guard footers` RunE to flip the
// exit code without a second, generic error line — the deny message on
// stderr, written before this is returned, is the whole report.
var errGuardFootersDenied = errors.New("guard footers: command publishes a banned footer")

// GuardFootersExitCode is the exit code `guard footers` returns when it
// denies a command, distinct from the generic 1 so a Claude Code
// PreToolUse hook can tell "denied" from "the guard itself broke".
const GuardFootersExitCode = 2

func newGuardCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "guard",
		Short: "Hook-invoked checks that decide whether Claude Code may run a command",
	}
	cmd.AddCommand(newGuardFootersCmd(env))
	return cmd
}

func newGuardFootersCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "footers",
		Short: "Deny a Bash command that would publish an authoring footer",
		Long: "Reads a Claude Code PreToolUse hook payload from stdin and denies a\n" +
			"git commit/tag or gh pr/issue/release/api call whose published text\n" +
			"carries a Co-Authored-By, Claude-Session, claude.ai/code/session, or\n" +
			"\"Generated with\" footer. Everything else is allowed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGuardFooters(env)
		},
	}
	return cmd
}

func runGuardFooters(env *Env) error {
	payload, err := io.ReadAll(env.Stdin)
	if err != nil {
		return fmt.Errorf("read hook payload: %w", err)
	}
	decision := guard.DecideFooters(payload)
	if !decision.Deny {
		return nil
	}
	fmt.Fprintf(env.Stderr, "blocked: command publishes %s — remove it and retry\n", decision.Pattern)
	return errGuardFootersDenied
}
