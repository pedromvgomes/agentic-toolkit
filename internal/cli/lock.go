package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/lockfile"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/sourcestore"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
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
	st, entryFS, entryName, err := loadStack(env)
	if err != nil {
		return err
	}
	cache, err := buildCache(cacheRoot)
	if err != nil {
		return err
	}
	plan, err := resolver.Resolve(st, entryFS, entryName, sourcestore.NewLiveProvider(cache))
	if err != nil {
		return fmt.Errorf("resolve: %w", err)
	}
	if !jsonOut {
		for _, d := range plan.Diagnostics {
			fmt.Fprintln(env.Stderr, "diag:", d.Message)
		}
	}
	resolved := plan.Lockfile()
	data, err := marshalLock(resolved, configFilePath(env))
	if err != nil {
		return err
	}
	path := lockfilePath(env)

	if frozen {
		return runLockFrozen(env, path, data, resolved, jsonOut)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil { // #nosec G306 -- 0644: agentic.lock in the user's repo, meant to be committed
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
	existing, readErr := os.ReadFile(path) // #nosec G304 -- reads the existing lockfile at the path the invoker named
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

// loadStack reads the entry-point stack file. With --config set, that's
// whatever path the user passed; otherwise it's `<WorkDir>/.agentic-
// toolkit.yaml`. The returned fs.FS is rooted at the manifest's
// directory so local `./...` refs in the manifest resolve from the
// right place — that's the config dir, not the apply dir.
//
// stack.ParseFile already returns a *ParseError whose Error() includes
// the path; we propagate it as-is to avoid duplicating the path in the
// rendered message.
func loadStack(env *Env) (*stack.Stack, fs.FS, string, error) {
	path := configFilePath(env)
	st, err := stack.ParseFile(path)
	if err != nil {
		return nil, nil, "", err
	}
	return st, os.DirFS(stackDir(env)), entryRelPath(env), nil
}

// buildCache resolves the cache root: explicit override wins, otherwise
// XDG-default location.
func buildCache(override string) (*sourcestore.Cache, error) {
	if override != "" {
		return sourcestore.NewCache(override), nil
	}
	return sourcestore.DefaultCache()
}

// marshalLock stamps the entry manifest's digest onto lock and renders it.
//
// Every writer goes through here. A lockfile written without the digest is
// indistinguishable from one predating the field, so the next run re-locks
// against the network, writes another digest-less lockfile, and the run after
// that does it again — an unbounded loop that looks exactly like the mtime
// behaviour this replaces.
//
// The digest is taken here rather than in the resolver because it is over the
// manifest's bytes on disk, and the resolver is handed a parsed stack.
func marshalLock(lock *lockfile.Lockfile, configPath string) ([]byte, error) {
	raw, err := os.ReadFile(configPath) // #nosec G304 -- reads the entry manifest at the path the invoker named
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", configPath, err)
	}
	lock.ConfigDigest = lockfile.Digest(raw)
	data, err := yaml.Marshal(lock)
	if err != nil {
		return nil, fmt.Errorf("marshal lockfile: %w", err)
	}
	return data, nil
}
