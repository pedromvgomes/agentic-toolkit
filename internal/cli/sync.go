package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/claude"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/sourcestore"
)

// newSyncCmd registers `agtk sync`: a one-shot lock-if-stale + fetch +
// render. Callers who want the individual primitives still have
// `agtk lock`, `agtk fetch`, and `agtk render`.
func newSyncCmd(env *Env) *cobra.Command {
	var (
		cacheRoot string
		scopeFlag string
		dryRun    bool
		force     bool
	)
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Lock-if-stale, fetch, and render in one shot",
		Long: "Convenience wrapper around `agtk lock`, `agtk fetch`, and\n" +
			"`agtk render`. If " + LockFileName + " is missing or older than\n" +
			ConfigFileName + ", agtk re-locks against the network. Then it\n" +
			"hydrates the cache from the lockfile and renders the resolved\n" +
			"plan to disk under the chosen scope.\n" +
			"\n" +
			"Use the individual subcommands (lock / fetch / render) when you\n" +
			"need to run them separately, e.g. in CI where lock and fetch run\n" +
			"in different jobs.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(env, cacheRoot, scopeFlag, dryRun, force)
		},
	}
	cmd.Flags().StringVar(&cacheRoot, "cache", "", "override cache root (defaults to $XDG_CACHE_HOME/agentic-toolkit)")
	cmd.Flags().StringVar(&scopeFlag, "scope", "project", "render scope: project or user")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report planned actions without writing")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files not tracked by agtk")
	return cmd
}

func runSync(env *Env, cacheRoot, scopeFlag string, dryRun, force bool) error {
	scope, err := parseScope(scopeFlag)
	if err != nil {
		return err
	}
	st, entryFS, entryName, err := loadStack(env)
	if err != nil {
		return err
	}
	cache, err := buildCache(cacheRoot)
	if err != nil {
		return err
	}

	configPath := configFilePath(env)
	lockPath := lockfilePath(env)
	stale, err := lockIsStale(configPath, lockPath)
	if err != nil {
		return err
	}

	if stale {
		fmt.Fprintln(env.Stdout, "sync: locking against the network")
		plan, err := resolver.Resolve(st, entryFS, entryName, sourcestore.NewLiveProvider(cache))
		if err != nil {
			return fmt.Errorf("lock: %w", err)
		}
		for _, d := range plan.Diagnostics {
			fmt.Fprintln(env.Stderr, "diag:", d.Message)
		}
		data, err := yaml.Marshal(plan.Lockfile())
		if err != nil {
			return fmt.Errorf("marshal lockfile: %w", err)
		}
		if err := os.WriteFile(lockPath, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", lockPath, err)
		}
	}

	lock, err := loadLockfile(env)
	if err != nil {
		return err
	}
	provider := sourcestore.NewFrozenProvider(cache, lock)
	if err := provider.Hydrate(); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	plan, err := resolver.Resolve(st, entryFS, entryName, provider)
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
		opts.ScopeRoot = filepath.Join(env.WorkDir, ".claude")
		opts.ProjectRoot = env.WorkDir
		opts.StackDir = stackDir(env)
	}
	if err := claude.Render(plan, opts); err != nil {
		return fmt.Errorf("render: %w", err)
	}
	return nil
}

// lockIsStale returns true when the lockfile is missing or older than the
// config. Stale → re-lock against the network. Errors that aren't
// fs.ErrNotExist propagate.
func lockIsStale(configPath, lockPath string) (bool, error) {
	cfgInfo, err := os.Stat(configPath)
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", configPath, err)
	}
	lockInfo, err := os.Stat(lockPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, fmt.Errorf("stat %s: %w", lockPath, err)
	}
	return cfgInfo.ModTime().After(lockInfo.ModTime()), nil
}
