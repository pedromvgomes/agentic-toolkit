package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/config"
	"github.com/pedromvgomes/agentic-toolkit/internal/lockfile"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/sourcestore"
)

func newLockCmd(env *Env) *cobra.Command {
	var (
		cacheRoot string
		frozen    bool
		jsonOut   bool
	)
	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Resolve sources to commit SHAs and write " + LockFileName,
		Long: "Reads " + ConfigFileName + ", resolves every source ref to its current\n" +
			"commit SHA via the network, hydrates the source cache, and writes\n" +
			LockFileName + ". Run this whenever the config changes or you want\n" +
			"to advance pinned refs to their current heads.\n" +
			"\n" +
			"With --frozen, agtk re-resolves but refuses to write: it compares the\n" +
			"would-be lockfile against the on-disk one and exits non-zero on any\n" +
			"difference. Intended as a CI guard alongside `agtk fetch`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLock(env, cacheRoot, frozen, jsonOut)
		},
	}
	cmd.Flags().StringVar(&cacheRoot, "cache", "", "override cache root (defaults to $XDG_CACHE_HOME/agentic-toolkit)")
	cmd.Flags().BoolVar(&frozen, "frozen", false, "fail if the lockfile would change instead of writing it")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	return cmd
}

func runLock(env *Env, cacheRoot string, frozen, jsonOut bool) error {
	cfg, err := loadConfig(env.WorkDir)
	if err != nil {
		return err
	}
	cache, err := buildCache(cacheRoot)
	if err != nil {
		return err
	}
	plan, err := resolver.Resolve(cfg, sourcestore.NewLiveProvider(cache))
	if err != nil {
		return fmt.Errorf("resolve: %w", err)
	}
	if !jsonOut {
		for _, d := range plan.Diagnostics {
			fmt.Fprintln(env.Stderr, "diag:", d.Message)
		}
	}
	resolved := plan.Lockfile()
	data, err := yaml.Marshal(resolved)
	if err != nil {
		return fmt.Errorf("marshal lockfile: %w", err)
	}
	path := filepath.Join(env.WorkDir, LockFileName)

	if frozen {
		return runLockFrozen(env, path, data, resolved, jsonOut)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if jsonOut {
		return writeLockJSON(env, lockJSON{
			Version:  jsonVersion,
			Action:   "wrote",
			Path:     path,
			Lockfile: lockfileJSON(resolved),
		})
	}
	fmt.Fprintln(env.Stdout, "wrote", path)
	return nil
}

// runLockFrozen compares the freshly-resolved lockfile bytes with the
// on-disk one. Equal → success. Different (or missing on-disk file) →
// non-zero exit with a drift report.
func runLockFrozen(env *Env, path string, resolved []byte, lock *lockfile.Lockfile, jsonOut bool) error {
	existing, readErr := os.ReadFile(path)
	switch {
	case errors.Is(readErr, fs.ErrNotExist):
		if jsonOut {
			_ = writeLockJSON(env, lockJSON{
				Version: jsonVersion,
				Action:  "drift",
				Path:    path,
				Drift:   "lockfile missing — run `agtk lock` to create it",
			})
		}
		return fmt.Errorf("--frozen: %s does not exist; run `agtk lock` to create it", path)
	case readErr != nil:
		return fmt.Errorf("read %s: %w", path, readErr)
	}
	if bytes.Equal(existing, resolved) {
		if jsonOut {
			return writeLockJSON(env, lockJSON{
				Version:  jsonVersion,
				Action:   "unchanged",
				Path:     path,
				Lockfile: lockfileJSON(lock),
			})
		}
		fmt.Fprintln(env.Stdout, "lockfile up to date")
		return nil
	}
	if jsonOut {
		_ = writeLockJSON(env, lockJSON{
			Version:  jsonVersion,
			Action:   "drift",
			Path:     path,
			Lockfile: lockfileJSON(lock),
			Drift:    "lockfile would change; run `agtk lock` to update",
		})
	}
	return fmt.Errorf("--frozen: %s would change; run `agtk lock` to update", path)
}

// loadConfig reads ConfigFileName from workDir. Returned errors carry
// the absolute path so users see a debuggable message.
func loadConfig(workDir string) (*config.ConsumerConfig, error) {
	path := filepath.Join(workDir, ConfigFileName)
	cfg, err := config.ParseFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return cfg, nil
}

// buildCache resolves the cache root: explicit override wins, otherwise
// XDG-default location.
func buildCache(override string) (*sourcestore.Cache, error) {
	if override != "" {
		return sourcestore.NewCache(override), nil
	}
	return sourcestore.DefaultCache()
}
