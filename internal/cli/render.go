package cli

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/claude"
	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/codex"
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/sourcestore"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
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
			"against the cache (frozen-lockfile mode), and writes each platform\n" +
			"named in the stack's platforms: (Claude Code only, when omitted) its\n" +
			"expected layout under the chosen scope:\n" +
			"  - project (default): <workdir>/.claude/ + <workdir>/CLAUDE.md, and\n" +
			"    (when codex is opted in) <workdir>/.agents/ + <workdir>/.codex/\n" +
			"    agents/ + <workdir>/AGENTS.md\n" +
			"  - user:              the same layouts under the home directory\n" +
			"\n" +
			"Whole-owned files (skills, agents, commands, rules) are tracked in a\n" +
			"sidecar .agtk-manifest.json per platform. Existing files not in the\n" +
			"manifest cause a refusal — pass --force to overwrite. CLAUDE.md and\n" +
			"settings.json use managed-region markers so user content outside\n" +
			"agtk's region is preserved on every render.",
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

	return renderPlatforms(st, plan, env, scope, dryRun, force)
}

// renderPlatforms dispatches plan to each of st.EffectivePlatforms()'s
// adapters, joining every failure the way each adapter already joins its
// own internal ones. A platform with no adapter here is a clear error,
// not a silent no-op — it may be a perfectly valid narrowing value on a
// definition's own platforms: allowlist, just not one this render dispatch
// recognizes as a target.
func renderPlatforms(st *stack.Stack, plan *resolver.Plan, env *Env, scope claude.Scope, dryRun, force bool) error {
	var errs []error
	for _, p := range st.EffectivePlatforms() {
		switch p {
		case definitions.PlatformClaude:
			opts := claude.Options{
				Scope:  scope,
				DryRun: dryRun,
				Force:  force,
				Stdout: env.Stdout,
			}
			if scope == claude.ScopeProject {
				// Pin the render to the consumer's working directory
				// rather than the renderer's os.Getwd(). Tests inject
				// env.WorkDir to a temp tree.
				opts.ScopeRoot = filepath.Join(env.WorkDir, ".claude")
				opts.ProjectRoot = env.WorkDir
			}
			if err := claude.Render(plan, opts); err != nil {
				errs = append(errs, fmt.Errorf("render (claude): %w", err))
			}
		case definitions.PlatformCodex:
			opts := codex.Options{
				Scope:  codexScope(scope),
				DryRun: dryRun,
				Force:  force,
				Stdout: env.Stdout,
			}
			if scope == claude.ScopeProject {
				opts.ProjectRoot = env.WorkDir
			}
			if err := codex.Render(plan, opts); err != nil {
				errs = append(errs, fmt.Errorf("render (codex): %w", err))
			}
		default:
			errs = append(errs, fmt.Errorf("render: no adapter for platform %q", p))
		}
	}
	return errors.Join(errs...)
}

// codexScope converts the flag-derived claude.Scope into codex's own
// Scope type. The two enums are isomorphic (project/user); parseScope
// stays the single place --scope's string value is validated.
func codexScope(s claude.Scope) codex.Scope {
	if s == claude.ScopeUser {
		return codex.ScopeUser
	}
	return codex.ScopeProject
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
