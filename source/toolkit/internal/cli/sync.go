package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/lockfile"
	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/sourcestore"
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
		data, err := marshalLock(plan.Lockfile(), configPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(lockPath, data, 0o644); err != nil { // #nosec G306 -- 0644: agentic.lock in the user's repo, meant to be committed
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

	return renderPlatforms(st, plan, env, scope, dryRun, force)
}

// lockIsStale returns true when the lockfile is missing, records no manifest
// digest, or records one that does not match the manifest on disk. Stale →
// re-lock against the network. Errors that aren't fs.ErrNotExist propagate.
//
// The comparison is over content, because the alternative — which of the two
// files has the later mtime — answers a different question and gets this one
// wrong in both directions. A checkout that writes the manifest after the
// lockfile makes an untouched pair look stale, and re-locking is the one
// branch here that reaches the network, so an offline runner fails on a repo
// whose committed lockfile was usable. An edit that preserves timestamps
// makes a mismatched pair look fresh, and sync then renders from pins that no
// longer describe the manifest without printing anything about it.
func lockIsStale(configPath, lockPath string) (bool, error) {
	cfg, err := os.ReadFile(configPath) // #nosec G304 -- reads the entry manifest at the path the invoker named
	if err != nil {
		return false, fmt.Errorf("read %s: %w", configPath, err)
	}
	lock, err := lockfile.ParseFile(lockPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) ||
			lockfile.IsKind(err, lockfile.ErrIO) {
			return true, nil
		}
		return false, err
	}
	// A lockfile recording no digest predates the field. Calling it fresh
	// would carry the mtime era's ambiguity forward for as long as nobody
	// re-locks; calling it stale costs one re-lock and then converges.
	return lock.ConfigDigest != lockfileConfigDigest(cfg), nil
}

// lockfileConfigDigest is the digest recorded in a lockfile for the entry
// manifest that produced it.
func lockfileConfigDigest(config []byte) string { return lockfile.Digest(config) }
