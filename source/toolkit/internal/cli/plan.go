package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/sourcestore"
)

func newPlanCmd(env *Env) *cobra.Command {
	var (
		cacheRoot string
		jsonOut   bool
		quiet     bool
	)
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Print the resolved plan for the current config + lockfile",
		Long: "Reads " + ConfigFileName + " and " + LockFileName + ", resolves every preset\n" +
			"entry against the cache (frozen-lockfile mode — no network, no ref\n" +
			"resolution), and prints the resulting plan: every source touched and\n" +
			"every definition that would render. Errors if the lockfile is missing\n" +
			"or any source in the config is not pinned (run `agtk lock`).\n" +
			"\n" +
			"Diagnostics (override losers, implicit-external sources) print to stderr\n" +
			"by default; --quiet suppresses them.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPlan(env, cacheRoot, jsonOut, quiet)
		},
	}
	cmd.Flags().StringVar(&cacheRoot, "cache", "", "override cache root (defaults to $XDG_CACHE_HOME/agentic-toolkit)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "suppress diagnostics on stderr")
	return cmd
}

func runPlan(env *Env, cacheRoot string, jsonOut, quiet bool) error {
	st, entryFS, entryName, err := loadStack(env)
	if err != nil {
		return err
	}
	lock, err := loadLockfile(env)
	if err != nil {
		return err
	}
	cache, err := buildCache(cacheRoot)
	if err != nil {
		return err
	}
	plan, err := resolver.Resolve(st, entryFS, entryName, sourcestore.NewFrozenProvider(cache, lock))
	if err != nil {
		return fmt.Errorf("resolve: %w", err)
	}
	if jsonOut {
		return writeJSON(env, planToJSON(plan))
	}
	printPlan(env, plan, quiet)
	return nil
}

func printPlan(env *Env, plan *resolver.Plan, quiet bool) {
	fmt.Fprintln(env.Stdout, "sources:")
	for _, s := range plan.Sources {
		fmt.Fprintf(env.Stdout, "  - [%s] %s@%s (%s)\n", s.Kind, s.URL, s.Ref, shortSHA(s.SHA))
	}
	fmt.Fprintln(env.Stdout, "definitions:")
	byCat := make(map[string][]resolver.PlannedDefinition, len(plan.Definitions))
	for _, d := range plan.Definitions {
		key := d.Category.CategoryDir()
		byCat[key] = append(byCat[key], d)
	}
	cats := make([]string, 0, len(byCat))
	for k := range byCat {
		cats = append(cats, k)
	}
	sort.Strings(cats)
	for _, cat := range cats {
		fmt.Fprintf(env.Stdout, "  %s:\n", cat)
		for _, d := range byCat[cat] {
			fmt.Fprintf(env.Stdout, "    - %s (stack:%s, source:%s)\n", d.Name, displayStack(d.StackName), d.SourceURL)
		}
	}
	if !quiet && len(plan.Diagnostics) > 0 {
		fmt.Fprintln(env.Stderr, "diagnostics:")
		for _, d := range plan.Diagnostics {
			fmt.Fprintf(env.Stderr, "  [%s] %s\n", d.Kind, d.Message)
		}
	}
}

// shortSHA returns the first 12 characters of a SHA, or the SHA itself
// if it's already shorter (defensive — lockfile SHAs are always full).
func shortSHA(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}

// displayStack renders a stack identifier for human-readable output.
// The empty string identifies the entry-point stack; rendered as
// "<entry>".
func displayStack(id string) string {
	if id == "" {
		return "<entry>"
	}
	return id
}
