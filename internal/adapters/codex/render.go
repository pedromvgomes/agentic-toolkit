// Package codex renders a resolved resolver.Plan to disk in Codex's
// expected layout.
//
// Every category this package handles is whole-owned: agtk owns the
// entire file, tracked in a sidecar manifest so a re-render can tell a
// file it wrote apart from one a user hand-authored. Unlike the Claude
// adapter's single scope-root, Codex splits its output across two roots
// that don't nest — skills and rules live under <ProjectRoot>/.agents,
// subagents under <ProjectRoot>/.codex/agents — plus AGENTS.md at
// <ProjectRoot> itself. The manifest that ties them together lives at
// <ProjectRoot>/.agents/.agtk-manifest.json but keys every entry relative
// to ProjectRoot, not to the directory the manifest sits in, so one
// render can detect a file stranded under either root once the
// definition that produced it is gone. The write/track machinery behind
// this is shared with every other platform adapter via
// internal/adapters/fsops; this package supplies only the category-to-
// directory mapping and the per-category content shape.
//
// The mcp, setting, and hook categories share one mixed-ownership file,
// .codex/config.toml — Codex has a single config surface where Claude
// has two — with ownership recorded per key path so a consumer's own
// config survives a re-render. See config.go.
//
// The command category has no Codex construct of its own; a command is
// converted to a skill, which is why the whole-owned plan writes two
// categories into .agents/skills. See command.go.
//
// AGENTS.md carries the instruction bodies plus an index of rules —
// Codex has no rules-discovery mechanism of its own, so the index is how
// a rule file is ever found. It has no preserve-outside-markers model
// like CLAUDE.md: the whole file is agtk's, rebuilt from the plan on
// every render, and (like any other whole-owned file) only written when
// there is something to put in it.
package codex

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/fsops"
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// Scope picks the render root. Project scope writes under the consumer's
// working directory; user scope writes under the home directory.
type Scope int

const (
	// ScopeProject renders into <workdir>/.agents, <workdir>/.codex/agents,
	// and <workdir>/AGENTS.md.
	ScopeProject Scope = iota
	// ScopeUser renders into ~/.agents, ~/.codex/agents, and ~/AGENTS.md.
	ScopeUser
)

// Options configures a render run.
type Options struct {
	// Scope picks the render root. Required.
	Scope Scope

	// ProjectRoot overrides the root all Codex output is rooted under.
	// Empty = derive from Scope (the working directory for project scope,
	// the home directory for user scope). Tests use this to render into
	// a temp dir.
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
		return errors.New("codex: nil plan")
	}
	rts, err := resolveRoots(opts)
	if err != nil {
		return err
	}

	ops, err := planWholeOwned(plan, rts, opts.Stdout)
	if err != nil {
		return err
	}

	manifest, err := wholeOps.ReadManifest(rts.AgentsRoot)
	if err != nil {
		return err
	}

	if !opts.Force {
		if cerrs := wholeOps.DetectCollisions(ops, manifest); len(cerrs) > 0 {
			return errors.Join(cerrs...)
		}
	}

	if opts.DryRun {
		reportDryRun(opts.Stdout, plan, ops, rts, manifest)
		return nil
	}

	if err := os.MkdirAll(rts.AgentsRoot, 0o755); err != nil { // #nosec G301 -- 0755: the agents root in the user's repo, meant to be committed
		return fmt.Errorf("codex: mkdir %s: %w", rts.AgentsRoot, err)
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
	errs = append(errs, wholeOps.RemoveStale(rts.ProjectRoot, manifest, newManifest, opts.Stdout)...)

	if err := renderConfig(plan, rts, opts); err != nil {
		errs = append(errs, err)
	}

	if err := wholeOps.WriteManifest(rts.AgentsRoot, newManifest); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// reportDryRun prints what each whole-owned file's intended action would
// be, plus the mixed-ownership config.toml a real render would touch,
// without writing anything.
func reportDryRun(stdout io.Writer, plan *resolver.Plan, ops []fsops.WholeOp, rts roots, manifest fsops.ManifestState) {
	wholeOps.ReportDryRunWholeOps(stdout, ops, rts.ProjectRoot, manifest)
	if stdout == nil {
		return
	}
	for _, d := range plan.Definitions {
		switch d.Category {
		case definitions.CategoryHook, definitions.CategoryMCP, definitions.CategorySetting:
			fmt.Fprintf(stdout, "would update %s (managed keys)\n", configPath(rts))
			return
		}
	}
}

// roots holds the resolved root directories for a render run. Unlike the
// Claude adapter's single ScopeRoot, AgentsRoot and SubagentsRoot don't
// nest — both are derived from ProjectRoot, and the manifest lives under
// AgentsRoot while every RelPath it tracks is ProjectRoot-relative.
type roots struct {
	ProjectRoot   string // <workdir> or the home directory (or override)
	AgentsRoot    string // <ProjectRoot>/.agents
	SubagentsRoot string // <ProjectRoot>/.codex/agents
}

// resolveRoots derives ProjectRoot (and the two roots under it) from opts.
func resolveRoots(opts Options) (roots, error) {
	r := roots{}
	if opts.ProjectRoot != "" {
		r.ProjectRoot = filepath.Clean(opts.ProjectRoot)
	} else {
		switch opts.Scope {
		case ScopeUser:
			home, err := os.UserHomeDir()
			if err != nil {
				return roots{}, fmt.Errorf("codex: resolve home dir: %w", err)
			}
			r.ProjectRoot = home
		case ScopeProject:
			wd, err := os.Getwd()
			if err != nil {
				return roots{}, fmt.Errorf("codex: resolve workdir: %w", err)
			}
			r.ProjectRoot = wd
		default:
			return roots{}, fmt.Errorf("codex: unknown scope %d", opts.Scope)
		}
	}
	r.AgentsRoot = filepath.Join(r.ProjectRoot, ".agents")
	r.SubagentsRoot = filepath.Join(r.ProjectRoot, ".codex", "agents")
	return r, nil
}
