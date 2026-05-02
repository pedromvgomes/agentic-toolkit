package claude

import (
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// Drift reports the per-bucket render-state divergence between a Plan
// and the on-disk render under a given Scope/Root. It is the structured
// surface consumed by `agtk status`. Equivalent to a Render(DryRun)
// summary, minus the human-readable formatting.
type Drift struct {
	// Missing lists tracked file paths (manifest-relative to ScopeRoot)
	// that no longer exist on disk.
	Missing []string

	// Modified lists tracked file paths whose on-disk content hash
	// differs from the manifest record (manual edit or external
	// rewrite).
	Modified []string

	// New lists file paths the plan would create that are not currently
	// tracked. Includes both pristine new renders and existing-but-
	// unmanaged collisions; status callers pair this with --force
	// awareness to interpret intent.
	New []string

	// Stale lists file paths in the manifest that are no longer in the
	// plan (would be removed on next render).
	Stale []string

	// HasInstructions / HasSettings record whether the plan contains
	// definitions that touch CLAUDE.md or settings.json. Status uses
	// these to decide whether to show "managed region present" hints.
	// These two paths are not whole-owned, so byte-level diffs are out
	// of scope here.
	HasInstructions bool
	HasSettings     bool
}

// Clean reports whether the drift is empty in every bucket.
func (d Drift) Clean() bool {
	return len(d.Missing) == 0 && len(d.Modified) == 0 && len(d.New) == 0 && len(d.Stale) == 0
}

// Diff computes the render-state drift for plan against opts. opts is
// interpreted exactly as Render does: ScopeRoot/ProjectRoot resolution,
// Scope-derived defaults, and the manifest at <ScopeRoot>/.agtk-manifest.json.
//
// Diff does not touch the filesystem beyond reading the manifest and
// stat'ing each tracked file. It does not consult settings.json or
// CLAUDE.md content (managed-region byte-equality is intentionally out
// of scope — see HasInstructions/HasSettings).
func Diff(plan *resolver.Plan, opts Options) (Drift, error) {
	if plan == nil {
		return Drift{}, errors.New("claude: nil plan")
	}
	roots, err := resolveRoots(opts)
	if err != nil {
		return Drift{}, err
	}
	manifest, err := readManifest(roots.ScopeRoot)
	if err != nil {
		return Drift{}, err
	}
	wholeOps, err := planWholeOwned(plan, roots)
	if err != nil {
		return Drift{}, err
	}

	planned := make(map[string]string, len(wholeOps))
	for _, op := range wholeOps {
		planned[op.RelPath] = contentHash(op.Content)
	}

	var d Drift

	// Tracked files: missing or modified relative to the manifest.
	trackedKeys := make([]string, 0, len(manifest.Files))
	for k := range manifest.Files {
		trackedKeys = append(trackedKeys, k)
	}
	sort.Strings(trackedKeys)
	for _, rel := range trackedKeys {
		recordedHash := manifest.Files[rel]
		abs := absPath(roots.ScopeRoot, rel)
		raw, err := os.ReadFile(abs)
		if err != nil {
			d.Missing = append(d.Missing, rel)
			continue
		}
		if contentHash(raw) != recordedHash {
			d.Modified = append(d.Modified, rel)
		}
	}

	// Files the plan would create that aren't tracked.
	plannedKeys := make([]string, 0, len(planned))
	for k := range planned {
		plannedKeys = append(plannedKeys, k)
	}
	sort.Strings(plannedKeys)
	for _, rel := range plannedKeys {
		if _, ok := manifest.Files[rel]; !ok {
			d.New = append(d.New, rel)
			continue
		}
		// Tracked but content has shifted in the plan: a re-render
		// would write different bytes. Surface as Modified too only
		// when on-disk content matches the manifest (otherwise the
		// user-modification signal already covers it).
		if recorded := manifest.Files[rel]; recorded != planned[rel] {
			abs := absPath(roots.ScopeRoot, rel)
			if raw, err := os.ReadFile(abs); err == nil && contentHash(raw) == recorded {
				if !contains(d.Modified, rel) {
					d.Modified = append(d.Modified, rel)
				}
			}
		}
	}

	// Manifest entries the plan no longer covers.
	for _, rel := range trackedKeys {
		if _, ok := planned[rel]; !ok {
			d.Stale = append(d.Stale, rel)
		}
	}

	for _, def := range plan.Definitions {
		switch def.Category {
		case definitions.CategoryInstruction:
			d.HasInstructions = true
		case definitions.CategoryHook, definitions.CategoryMCP, definitions.CategorySetting:
			d.HasSettings = true
		}
	}
	return d, nil
}

func absPath(scopeRoot, rel string) string {
	return filepath.Join(scopeRoot, filepath.FromSlash(rel))
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
