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
//     refusal unless Options.Force is set. The write/track machinery
//     behind this model is shared with every other platform adapter via
//     internal/adapters/fsops — this package supplies only the category-
//     to-directory mapping and the per-category frontmatter shape.
//  2. Managed-region files (CLAUDE.md). agtk owns only the region between
//     <!-- BEGIN AGTK MANAGED --> and <!-- END AGTK MANAGED -->; content
//     outside the markers is preserved verbatim. When CLAUDE.md does not
//     exist, project-scope renders create it with just the managed
//     block — CLAUDE.md never reads or references AGENTS.md, which (when
//     a stack also renders for the codex platform) is a wholly separate,
//     independently-generated file.
//  3. Mixed-ownership JSON (settings.json, .mcp.json). agtk owns only
//     the top-level keys it has rendered in each file, recorded in
//     `_meta.agtk.managed`. User keys are preserved. MCP servers render
//     to project-scope .mcp.json exclusively — Claude Code does not
//     read project MCP servers from settings.json.
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

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/adapters/fsops"
	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/resolver"
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

	// ProjectRoot overrides the project root CLAUDE.md is written to
	// under project scope. Empty = parent of ScopeRoot. Ignored under
	// user scope (CLAUDE.md always lives inside ScopeRoot).
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
	ops, err := planWholeOwned(plan, roots)
	if err != nil {
		return err
	}

	manifest, err := wholeOps.ReadManifest(roots.ScopeRoot)
	if err != nil {
		return err
	}

	if !opts.Force {
		if cerrs := wholeOps.DetectCollisions(ops, manifest); len(cerrs) > 0 {
			return errors.Join(cerrs...)
		}
	}

	if opts.DryRun {
		return reportDryRun(opts.Stdout, plan, ops, roots, manifest)
	}

	if err := os.MkdirAll(roots.ScopeRoot, 0o755); err != nil { // #nosec G301 -- 0755: the scope root in the user's repo, meant to be committed
		return fmt.Errorf("claude: mkdir %s: %w", roots.ScopeRoot, err)
	}

	var errs []error
	newManifest := fsops.NewManifestState()
	for _, op := range ops {
		if err := wholeOps.ApplyWholeOp(op, newManifest, opts.Stdout); err != nil {
			errs = append(errs, err)
		}
	}

	// Stale-cleanup: paths that were tracked last time but are not in
	// this render. Remove only files we own (manifest tracked).
	errs = append(errs, wholeOps.RemoveStale(roots.ScopeRoot, manifest, newManifest, opts.Stdout)...)

	if err := renderInstructions(plan, roots, opts); err != nil {
		errs = append(errs, err)
	}

	if err := renderSettings(plan, roots, opts); err != nil {
		errs = append(errs, err)
	}

	if err := renderMCP(plan, roots, opts); err != nil {
		errs = append(errs, err)
	}

	if err := wholeOps.WriteManifest(roots.ScopeRoot, newManifest); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// reportDryRun prints what each whole-owned file's intended action would
// be, plus the mixed-ownership targets (CLAUDE.md, settings.json) that a
// real render would touch, without writing anything.
//
// The mixed-ownership JSON is parsed here for the reason Options.DryRun
// states: a render reads settings.json and .mcp.json whether or not it
// has anything to put in them, so a file that will not parse is a
// failure this preview can see, and reporting success for one previews a
// render that will not happen.
func reportDryRun(stdout io.Writer, plan *resolver.Plan, ops []fsops.WholeOp, roots scopeRoots, manifest fsops.ManifestState) error {
	wholeOps.ReportDryRunWholeOps(stdout, ops, roots.ScopeRoot, manifest)

	if _, err := readSettings(settingsPath(roots)); err != nil {
		return err
	}
	if roots.Scope == ScopeProject {
		if _, err := readSettings(mcpJSONPath(roots)); err != nil {
			return err
		}
	}

	if stdout == nil {
		return nil
	}

	// Settings + CLAUDE.md preview. Cheap but accurate enough: just
	// announce the targets — actual diff would require running the
	// merge logic without writing.
	hasInstr := false
	hasSettings := false
	for _, d := range plan.Definitions {
		switch d.Category {
		case definitions.CategoryInstruction:
			hasInstr = true
		case definitions.CategoryHook, definitions.CategoryMCP, definitions.CategorySetting:
			hasSettings = true
		}
	}
	if hasInstr {
		fmt.Fprintf(stdout, "would update %s (managed region)\n", instructionsPath(roots))
	}
	if hasSettings {
		fmt.Fprintf(stdout, "would update %s (managed top-level keys)\n", settingsPath(roots))
	}
	return nil
}

// scopeRoots holds the resolved root directories for a render run.
type scopeRoots struct {
	Scope       Scope
	ScopeRoot   string // <workdir>/.claude or ~/.claude (or override)
	ProjectRoot string // <workdir> or ~/.claude
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
		// User scope: CLAUDE.md lives inside ScopeRoot.
		roots.ProjectRoot = roots.ScopeRoot
	} else {
		roots.ProjectRoot = filepath.Dir(roots.ScopeRoot)
	}
	return roots, nil
}
