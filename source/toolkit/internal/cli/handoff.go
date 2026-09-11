package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/handoff"
	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/review"
)

func newHandoffCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "handoff",
		Short: "Work with the handoff documents in this worktree",
	}
	cmd.AddCommand(newHandoffListCmd(env))
	return cmd
}

func newHandoffListCmd(env *Env) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the handoffs a session may act on, and name what was refused",
		Long: "Reports the documents in handoff/ that this session may act on.\n" +
			"\n" +
			"A handoff is written locally and never committed, so one that git tracks\n" +
			"arrived with a branch rather than from a session on this machine — and a\n" +
			"handoff directs subagents holding Write, Edit and Bash. Tracked-ness alone\n" +
			"is not the test: git tracks paths, so a committed symlink at handoff/ leaves\n" +
			"the path handoff/x.md untracked while its content is entirely branch-authored.\n" +
			"\n" +
			"A symlink is refused rather than followed, here as in a review root, and\n" +
			"every refusal is named with its reason. A document withheld in silence and\n" +
			"a directory holding nothing produce the same empty list, and only one of\n" +
			"them means there is no work waiting.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHandoffList(env, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	return cmd
}

type handoffRefusedJSON struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type handoffListJSON struct {
	Version  int                  `json:"version"`
	Handoffs []string             `json:"handoffs"`
	Refused  []handoffRefusedJSON `json:"refused"`
}

func runHandoffList(env *Env, jsonOut bool) error {
	root, err := review.RepoRoot(env.WorkDir)
	if err != nil {
		return fmt.Errorf("locate the repository: %w", err)
	}
	docs, refused, err := handoff.List(root)
	if err != nil {
		return err
	}

	if jsonOut {
		out := handoffListJSON{Version: jsonVersion, Handoffs: []string{}, Refused: []handoffRefusedJSON{}}
		for _, d := range docs {
			out.Handoffs = append(out.Handoffs, d.Path)
		}
		for _, r := range refused {
			out.Refused = append(out.Refused, handoffRefusedJSON{Path: r.Path, Reason: r.Reason})
		}
		return writeJSON(env, out)
	}

	// %q, not %s. A refusal is by construction about a branch-authored path,
	// the session-start hook pipes this output into a fresh session's context,
	// and that session dispatches subagents holding Write, Edit and Bash. A
	// filename carrying newlines would otherwise write its own lines into that
	// context. Quoting is what reviewrun does with a path it refuses to write.
	for _, d := range docs {
		fmt.Fprintf(env.Stdout, "%q\n", d.Path)
	}
	for _, r := range refused {
		fmt.Fprintf(env.Stderr, "refused %q — %s\n", r.Path, r.Reason)
	}
	return nil
}
