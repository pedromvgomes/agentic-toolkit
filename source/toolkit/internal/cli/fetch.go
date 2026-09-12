package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/lockfile"
	"github.com/pedromvgomes/agentic-toolkit/internal/sourcestore"
)

func newFetchCmd(env *Env) *cobra.Command {
	var cacheRoot string
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Hydrate the source cache from " + LockFileName,
		Long: "Reads " + LockFileName + " and ensures every pinned (URL, SHA) is\n" +
			"present in the cache, fetching any that are missing. Errors if the\n" +
			"lockfile is absent — run `agtk lock` first. Useful for fresh clones\n" +
			"and CI runs that should not perform ref resolution.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFetch(env, cacheRoot)
		},
	}
	cmd.Flags().StringVar(&cacheRoot, "cache", "", "override cache root (defaults to $XDG_CACHE_HOME/agentic-toolkit)")
	return cmd
}

func runFetch(env *Env, cacheRoot string) error {
	lock, err := loadLockfile(env)
	if err != nil {
		return err
	}
	cache, err := buildCache(cacheRoot)
	if err != nil {
		return err
	}
	provider := sourcestore.NewFrozenProvider(cache, lock)
	if err := provider.Hydrate(); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "fetched %d source(s) into %s\n", len(lock.Sources), cache.Root())
	return nil
}

// loadLockfile reads the lockfile from the same directory as the
// entry manifest (stackDir). A missing lockfile is reported with a
// clear "run agtk lock" hint so downstream commands don't have to
// repeat the message.
func loadLockfile(env *Env) (*lockfile.Lockfile, error) {
	path := lockfilePath(env)
	lock, err := lockfile.ParseFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s not found; run `agtk lock` first", path)
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return lock, nil
}
