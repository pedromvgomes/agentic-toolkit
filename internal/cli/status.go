package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/claude"
	"github.com/pedromvgomes/agentic-toolkit/internal/lockfile"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/sourcestore"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
)

func newStatusCmd(env *Env) *cobra.Command {
	var (
		cacheRoot string
		scopeFlag string
		jsonOut   bool
	)
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report drift between config, lockfile, cache, and rendered state",
		Long: "Compares three pairs and surfaces any drift:\n" +
			"  - config vs lockfile     — sources in " + ConfigFileName + " missing or\n" +
			"                             changed in " + LockFileName + "\n" +
			"  - lockfile vs cache      — pinned SHAs missing locally (run `agtk fetch`)\n" +
			"  - rendered state vs plan — files agtk would create/modify/remove on next\n" +
			"                             `agtk render` (manifest-tracked files only;\n" +
			"                             settings.json/CLAUDE.md drift is summarized,\n" +
			"                             not byte-diffed)\n" +
			"\n" +
			"Exits 0 when all three buckets are clean; non-zero otherwise.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(env, cacheRoot, scopeFlag, jsonOut)
		},
	}
	cmd.Flags().StringVar(&cacheRoot, "cache", "", "override cache root (defaults to $XDG_CACHE_HOME/agentic-toolkit)")
	cmd.Flags().StringVar(&scopeFlag, "scope", "project", "render scope: project or user")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	return cmd
}

func runStatus(env *Env, cacheRoot, scopeFlag string, jsonOut bool) error {
	scope, err := parseScope(scopeFlag)
	if err != nil {
		return err
	}

	st, entryFS, entryName, err := loadStack(env.WorkDir)
	if err != nil {
		return err
	}
	lock, lockErr := loadLockfileIfPresent(env.WorkDir)
	cache, err := buildCache(cacheRoot)
	if err != nil {
		return err
	}

	bucket1 := diffStackVsLockfile(st, lock, lockErr)

	var (
		bucket2 []string
		bucket3 []string
		render  *claude.Drift
	)

	// Bucket 2 + 3 require a lockfile to compute. If absent, surface
	// that fact in bucket 1 and skip the rest.
	if lock != nil {
		bucket2 = diffLockfileVsCache(lock, cache)

		// Re-resolve from the cache to drive the render-state diff.
		// Resolver errors are non-fatal here — we surface them as drift
		// rather than aborting the status report.
		plan, rerr := resolver.Resolve(st, entryFS, entryName, sourcestore.NewFrozenProvider(cache, lock))
		if rerr != nil {
			bucket3 = []string{fmt.Sprintf("resolve: %v", rerr)}
		} else {
			scopeRoot, projectRoot := renderRoots(env.WorkDir, scope)
			d, derr := claude.Diff(plan, claude.Options{
				Scope:       scope,
				ScopeRoot:   scopeRoot,
				ProjectRoot: projectRoot,
			})
			if derr != nil {
				bucket3 = []string{fmt.Sprintf("diff: %v", derr)}
			} else {
				render = &d
				bucket3 = formatRenderDrift(d)
			}
		}
	}

	clean := len(bucket1) == 0 && len(bucket2) == 0 && len(bucket3) == 0

	if jsonOut {
		out := statusJSON{
			Version: jsonVersion,
			Clean:   clean,
			Drift: driftJSON{
				ConfigVsLockfile: nilToEmpty(bucket1),
				LockfileVsCache:  nilToEmpty(bucket2),
				Render:           nilToEmpty(bucket3),
			},
		}
		if err := writeJSON(env, out); err != nil {
			return err
		}
		if !clean {
			return errStatusDrift
		}
		return nil
	}

	printStatusBuckets(env, bucket1, bucket2, bucket3, render)
	if !clean {
		return errStatusDrift
	}
	fmt.Fprintln(env.Stdout, "ok")
	return nil
}

// errStatusDrift is the sentinel returned when status finds drift. The
// CLI's Execute() prints any error with `agtk:` prefix; for status that
// would be noisy because the bucket lists already explain things. Use
// a typed error and suppress the prefix in main when matched.
var errStatusDrift = errors.New("status: drift detected")

// loadLockfileIfPresent is a softer variant of loadLockfile: a missing
// file returns (nil, nil). Other parse errors propagate.
func loadLockfileIfPresent(workDir string) (*lockfile.Lockfile, error) {
	path := filepath.Join(workDir, LockFileName)
	lock, err := lockfile.ParseFile(path)
	if err == nil {
		return lock, nil
	}
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
		return nil, fs.ErrNotExist
	}
	return nil, fmt.Errorf("read %s: %w", path, err)
}

// diffStackVsLockfile flags every URL referenced from the entry-point
// stack (extends + per-category URL entries) that is missing or has a
// divergent ref in the lockfile. Sources in the lockfile but not in the
// stack are not flagged here — they are normal artifacts of recursive
// extends resolution recorded at lock time.
//
// Local-path imports and bare-name entries don't reach the network and
// don't appear in the lockfile, so they are skipped. Recursive extends
// inside imported stacks are also skipped: status only inspects the
// top-level entry-point file, so a missing pin for a transitive import
// will surface in the next bucket (lockfile vs cache) as a fetch error
// instead.
func diffStackVsLockfile(st *stack.Stack, lock *lockfile.Lockfile, lockErr error) []string {
	if lock == nil {
		if errors.Is(lockErr, fs.ErrNotExist) {
			return []string{LockFileName + " missing — run `agtk lock`"}
		}
		return []string{fmt.Sprintf("lockfile: %v", lockErr)}
	}
	byKey := map[string]string{} // url@ref -> sha
	for _, s := range lock.Sources {
		byKey[s.URL+"@"+s.Ref] = s.SHA
	}
	byURL := map[string][]string{}
	for _, s := range lock.Sources {
		byURL[s.URL] = append(byURL[s.URL], s.Ref)
	}

	var drift []string
	check := func(url, ref string) {
		if url == "" {
			return
		}
		if _, ok := byKey[url+"@"+ref]; ok {
			return
		}
		if ref == "" {
			if pins := byURL[url]; len(pins) == 1 {
				return
			}
		}
		drift = append(drift, fmt.Sprintf("source %s@%s not pinned in lockfile", url, displayRef(ref)))
	}

	// extends URLs.
	for _, ext := range st.Extends {
		if !ext.IsExternal() {
			continue
		}
		repoURL, _ := splitGitURLForStatus(ext.URL)
		check(repoURL, ext.Ref)
	}

	// Per-category URL entries.
	for _, entry := range allEntries(st) {
		if !entry.IsExternal() {
			continue
		}
		repoURL, _ := splitGitURLForStatus(entry.URL)
		check(repoURL, entry.Ref)
	}

	return drift
}

// allEntries flattens every per-category entry list into a single slice.
func allEntries(st *stack.Stack) []stack.EntryRef {
	var out []stack.EntryRef
	out = append(out, st.Skills...)
	out = append(out, st.Agents...)
	out = append(out, st.Rules...)
	out = append(out, st.Instructions...)
	out = append(out, st.Commands...)
	out = append(out, st.Hooks...)
	out = append(out, st.MCP...)
	out = append(out, st.Settings...)
	return out
}

// splitGitURLForStatus mirrors resolver.splitGitURL — local copy to
// avoid an import cycle.
func splitGitURLForStatus(u string) (repoURL, inRepoPath string) {
	const sep = ".git/"
	for i := 0; i+len(sep) <= len(u); i++ {
		if u[i:i+len(sep)] == sep {
			return u[:i+len(".git")], u[i+len(sep):]
		}
	}
	return u, ""
}

// diffLockfileVsCache reports each pinned source whose worktree is
// absent from the cache.
func diffLockfileVsCache(lock *lockfile.Lockfile, cache *sourcestore.Cache) []string {
	var drift []string
	for _, s := range lock.Sources {
		if !cache.HasSource(s.URL, s.SHA) {
			drift = append(drift, fmt.Sprintf("source %s@%s (%s) missing from cache — run `agtk fetch`", s.URL, s.Ref, shortSHA(s.SHA)))
		}
	}
	return drift
}

// formatRenderDrift turns a claude.Drift into one human-readable line
// per affected file plus optional managed-region lines.
func formatRenderDrift(d claude.Drift) []string {
	var out []string
	for _, p := range d.New {
		out = append(out, "would write "+p)
	}
	for _, p := range d.Modified {
		out = append(out, "would update "+p)
	}
	for _, p := range d.Missing {
		out = append(out, "tracked file missing on disk: "+p)
	}
	for _, p := range d.Stale {
		out = append(out, "would remove "+p)
	}
	sort.Strings(out)
	return out
}

func printStatusBuckets(env *Env, b1, b2, b3 []string, render *claude.Drift) {
	printBucket(env, "config vs lockfile", b1)
	printBucket(env, "lockfile vs cache", b2)
	printBucket(env, "rendered state", b3)
	if render != nil {
		if render.HasInstructions {
			fmt.Fprintln(env.Stdout, "  CLAUDE.md: managed region present in plan")
		}
		if render.HasSettings {
			fmt.Fprintln(env.Stdout, "  settings.json: managed top-level keys present in plan")
		}
	}
}

func printBucket(env *Env, name string, lines []string) {
	fmt.Fprintf(env.Stdout, "%s:\n", name)
	if len(lines) == 0 {
		fmt.Fprintln(env.Stdout, "  clean")
		return
	}
	for _, l := range lines {
		fmt.Fprintf(env.Stdout, "  - %s\n", l)
	}
}

// renderRoots derives the same scope roots the renderer uses so status
// reads the right manifest.
func renderRoots(workDir string, scope claude.Scope) (scopeRoot, projectRoot string) {
	switch scope {
	case claude.ScopeProject:
		return filepath.Join(workDir, ".claude"), workDir
	case claude.ScopeUser:
		// Return empty strings so resolveRoots falls back to ~/.claude.
		return "", ""
	}
	return "", ""
}

func displayRef(ref string) string {
	if ref == "" {
		return "<default>"
	}
	return ref
}

func nilToEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
