// Package claude renders a resolved resolver.Plan to disk in Claude
// Code's expected layout.
//
// Three ownership models coexist under a single scope-root:
//
//  1. Whole-owned files (skill, agent, command, rule). agtk owns the
//     entire file. A sidecar manifest at <scope-root>/.agtk-manifest.json
//     records which paths were written and their content hashes; on
//     re-render, files in the manifest can be overwritten freely, files
//     present on disk but absent from the manifest trigger a collision
//     refusal unless Options.Force is set.
//  2. Managed-region files (CLAUDE.md). agtk owns only the region between
//     <!-- BEGIN AGTK MANAGED --> and <!-- END AGTK MANAGED -->; content
//     outside the markers is preserved verbatim. When CLAUDE.md does not
//     exist, project-scope renders seed it from AGENTS.md (via @AGENTS.md
//     import) when present, else create a fresh file.
//  3. Mixed-ownership JSON (settings.json). agtk owns only the top-level
//     keys it has rendered, recorded in `_meta.agtk.managed`. User keys
//     are preserved.
//
// Bundle companion files (skills/agents) are copied verbatim from
// PlannedDefinition.SourceFS, walking path.Dir(EntryPath) and skipping
// the entry file itself (which is reconstructed from the parsed
// Definition).
package claude

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// Scope picks the render root. Project scope writes under the consumer's
// working directory; user scope writes under ~/.claude.
type Scope int

const (
	// ScopeProject renders into <workdir>/.claude (and <workdir>/CLAUDE.md
	// for instructions).
	ScopeProject Scope = iota
	// ScopeUser renders into ~/.claude (and ~/.claude/CLAUDE.md for
	// instructions).
	ScopeUser
)

// Options configures a render run.
type Options struct {
	// Scope picks the render root. Required.
	Scope Scope

	// ScopeRoot overrides the scope-derived root directory. Empty =
	// derive from Scope. Tests use this to render into a temp dir.
	ScopeRoot string

	// ProjectRoot overrides the project root used for CLAUDE.md /
	// AGENTS.md lookup under project scope. Empty = parent of ScopeRoot.
	// Ignored under user scope (CLAUDE.md always lives inside ScopeRoot).
	ProjectRoot string

	// DryRun reports what would change without touching the filesystem.
	// Errors that depend on filesystem state (collision refusal,
	// directory creation, manifest read) are still surfaced.
	DryRun bool

	// Force overrides the whole-owned-file collision refusal: existing
	// files not tracked in the manifest will be overwritten.
	Force bool

	// Stdout receives a per-action summary line for each write/skip.
	// Nil silences output.
	Stdout io.Writer
}

// Render writes plan to disk per opts. Returns errors.Join of all
// failures; partial writes are not rolled back, but collision refusals
// happen up front before any write.
func Render(plan *resolver.Plan, opts Options) error {
	if plan == nil {
		return errors.New("claude: nil plan")
	}
	roots, err := resolveRoots(opts)
	if err != nil {
		return err
	}

	// Plan all whole-owned-file writes first so collisions are surfaced
	// before any filesystem mutation. Settings/CLAUDE.md follow.
	wholeOps, err := planWholeOwned(plan, roots)
	if err != nil {
		return err
	}

	manifest, err := readManifest(roots.ScopeRoot)
	if err != nil {
		return err
	}

	if !opts.Force {
		if cerrs := detectCollisions(wholeOps, manifest); len(cerrs) > 0 {
			return errors.Join(cerrs...)
		}
	}

	if opts.DryRun {
		reportDryRun(opts.Stdout, plan, wholeOps, roots, manifest)
		return nil
	}

	if err := os.MkdirAll(roots.ScopeRoot, 0o755); err != nil {
		return fmt.Errorf("claude: mkdir %s: %w", roots.ScopeRoot, err)
	}

	var errs []error
	newManifest := newManifestState()
	for _, op := range wholeOps {
		if err := applyWholeOp(op, newManifest, opts); err != nil {
			errs = append(errs, err)
		}
	}

	// Stale-cleanup: paths that were tracked last time but are not in
	// this render. Remove only files we own (manifest tracked).
	for path := range manifest.Files {
		if _, kept := newManifest.Files[path]; kept {
			continue
		}
		full := filepath.Join(roots.ScopeRoot, path)
		if rerr := os.Remove(full); rerr != nil && !os.IsNotExist(rerr) {
			errs = append(errs, fmt.Errorf("claude: remove stale %s: %w", full, rerr))
		} else if opts.Stdout != nil {
			fmt.Fprintf(opts.Stdout, "removed %s\n", full)
		}
	}

	if err := renderInstructions(plan, roots, opts); err != nil {
		errs = append(errs, err)
	}

	if err := renderSettings(plan, roots, opts); err != nil {
		errs = append(errs, err)
	}

	if err := writeManifest(roots.ScopeRoot, newManifest); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// scopeRoots holds the resolved root directories for a render run.
type scopeRoots struct {
	Scope       Scope
	ScopeRoot   string // <workdir>/.claude or ~/.claude (or override)
	ProjectRoot string // <workdir> or ~/.claude (no AGENTS.md fallback under user scope)
}

// resolveRoots derives ScopeRoot and ProjectRoot from opts.
func resolveRoots(opts Options) (scopeRoots, error) {
	roots := scopeRoots{Scope: opts.Scope}
	if opts.ScopeRoot != "" {
		roots.ScopeRoot = filepath.Clean(opts.ScopeRoot)
	} else {
		switch opts.Scope {
		case ScopeUser:
			home, err := os.UserHomeDir()
			if err != nil {
				return scopeRoots{}, fmt.Errorf("claude: resolve home dir: %w", err)
			}
			roots.ScopeRoot = filepath.Join(home, ".claude")
		case ScopeProject:
			wd, err := os.Getwd()
			if err != nil {
				return scopeRoots{}, fmt.Errorf("claude: resolve workdir: %w", err)
			}
			roots.ScopeRoot = filepath.Join(wd, ".claude")
		default:
			return scopeRoots{}, fmt.Errorf("claude: unknown scope %d", opts.Scope)
		}
	}
	if opts.ProjectRoot != "" {
		roots.ProjectRoot = filepath.Clean(opts.ProjectRoot)
	} else if opts.Scope == ScopeUser {
		// User scope: CLAUDE.md lives inside ScopeRoot; project root is
		// the same dir for the AGENTS.md (non-)lookup path. The
		// instructions renderer suppresses the AGENTS.md seed under
		// user scope regardless.
		roots.ProjectRoot = roots.ScopeRoot
	} else {
		roots.ProjectRoot = filepath.Dir(roots.ScopeRoot)
	}
	return roots, nil
}
