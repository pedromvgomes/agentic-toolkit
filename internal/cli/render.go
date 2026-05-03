package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/claude"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/sourcestore"
)

func newRenderCmd(env *Env) *cobra.Command {
	var (
		cacheRoot string
		scopeFlag string
		dryRun    bool
		force     bool
	)
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Render the resolved plan to disk under .claude/",
		Long: "Reads " + ConfigFileName + " and " + LockFileName + ", resolves the plan\n" +
			"against the cache (frozen-lockfile mode), and writes Claude Code's\n" +
			"expected layout under the chosen scope:\n" +
			"  - project (default): <workdir>/.claude/ + <workdir>/CLAUDE.md\n" +
			"  - user:              ~/.claude/ + ~/.claude/CLAUDE.md\n" +
			"\n" +
			"Whole-owned files (skills, agents, commands, rules) are tracked in a\n" +
			"sidecar .agtk-manifest.json. Existing files not in the manifest cause\n" +
			"a refusal — pass --force to overwrite. CLAUDE.md and settings.json use\n" +
			"managed-region markers so user content outside agtk's region is\n" +
			"preserved on every render.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRender(env, cacheRoot, scopeFlag, dryRun, force)
		},
	}
	cmd.Flags().StringVar(&cacheRoot, "cache", "", "override cache root (defaults to $XDG_CACHE_HOME/agentic-toolkit)")
	cmd.Flags().StringVar(&scopeFlag, "scope", "project", "render scope: project or user")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report planned actions without writing")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files not tracked by agtk")
	return cmd
}

func runRender(env *Env, cacheRoot, scopeFlag string, dryRun, force bool) error {
	scope, err := parseScope(scopeFlag)
	if err != nil {
		return err
	}
	st, entryFS, entryName, err := loadStack(env.WorkDir)
	if err != nil {
		return err
	}
	lock, err := loadLockfile(env.WorkDir)
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

	opts := claude.Options{
		Scope:  scope,
		DryRun: dryRun,
		Force:  force,
		Stdout: env.Stdout,
	}
	if scope == claude.ScopeProject {
		// Pin the render to the consumer's working directory rather
		// than the renderer's os.Getwd(). Tests inject env.WorkDir to
		// a temp tree.
		opts.ScopeRoot = env.WorkDir + "/.claude"
		opts.ProjectRoot = env.WorkDir
	}
	if err := claude.Render(plan, opts); err != nil {
		return fmt.Errorf("render: %w", err)
	}
	return nil
}

func parseScope(s string) (claude.Scope, error) {
	switch s {
	case "project", "":
		return claude.ScopeProject, nil
	case "user":
		return claude.ScopeUser, nil
	}
	return 0, fmt.Errorf("invalid --scope %q (want project or user)", s)
}
