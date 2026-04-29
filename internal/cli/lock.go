package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/config"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/sourcestore"
)

func newLockCmd(env *Env) *cobra.Command {
	var cacheRoot string
	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Resolve sources to commit SHAs and write " + LockFileName,
		Long: "Reads " + ConfigFileName + ", resolves every source ref to its current\n" +
			"commit SHA via the network, hydrates the source cache, and writes\n" +
			LockFileName + ". Run this whenever the config changes or you want\n" +
			"to advance pinned refs to their current heads.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLock(env, cacheRoot)
		},
	}
	cmd.Flags().StringVar(&cacheRoot, "cache", "", "override cache root (defaults to $XDG_CACHE_HOME/agentic-toolkit)")
	return cmd
}

func runLock(env *Env, cacheRoot string) error {
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
	for _, d := range plan.Diagnostics {
		fmt.Fprintln(env.Stderr, "diag:", d.Message)
	}
	data, err := yaml.Marshal(plan.Lockfile())
	if err != nil {
		return fmt.Errorf("marshal lockfile: %w", err)
	}
	path := filepath.Join(env.WorkDir, LockFileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Fprintln(env.Stdout, "wrote", path)
	return nil
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
