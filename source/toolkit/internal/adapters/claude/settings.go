package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// The settings keys the memory grants are appended under, named rather than
// spelled out at each use so the two sites cannot drift apart.
const (
	permissionsKey = "permissions"
	allowKey       = "allow"

	// memoryExplorerAgent is the definition the store grants exist for.
	memoryExplorerAgent = "memory-explorer"
)

// settingsPath returns the absolute target path for settings.json.
func settingsPath(roots scopeRoots) string {
	return filepath.Join(roots.ScopeRoot, "settings.json")
}

// renderSettings updates settings.json's managed top-level keys.
//
// Ownership is recorded in `_meta.agtk.managed` (sorted list of
// top-level keys agtk wrote). On every render we drop every key listed
// there, then write the new managed keys, then rewrite the marker. Keys
// not in either the old or new managed list are preserved verbatim.
//
// Conflict resolution between hook/setting contributions to the same
// top-level key:
//   - Hook definitions own `hooks` exclusively. If a setting tries to
//     write `hooks`, the hook definitions win (the setting's `hooks`
//     fragment is dropped silently).
//   - For other top-level keys touched by multiple settings, last by
//     preset stack order wins (preset index lookup against
//     plan.Config.Presets), with stable PlannedDefinition order as
//     tiebreak.
//
// MCP definitions do not touch settings.json at all — see mcp.go. Claude
// Code does not read project MCP servers from settings.json.
func renderSettings(plan *resolver.Plan, roots scopeRoots, opts Options) error {
	hooks := collectHooks(plan)
	settingFragments := collectSettingFragments(plan)
	if err := addMemoryGrants(settingFragments, plan); err != nil {
		return err
	}

	if len(hooks) == 0 && len(settingFragments) == 0 {
		return clearSettingsManaged(roots, opts)
	}

	target := settingsPath(roots)
	current, err := readSettings(target)
	if err != nil {
		return err
	}

	// Drop all previously-managed keys so renames between renders don't
	// leave stale top-level keys behind.
	prevManaged := readManagedList(current)
	for _, k := range prevManaged {
		delete(current, k)
	}

	managed := map[string]bool{}
	if len(hooks) > 0 {
		current["hooks"] = hooks
		managed["hooks"] = true
	}
	for key, value := range settingFragments {
		if managed[key] {
			// hook already claimed this key; setting contribution is
			// suppressed for safety.
			continue
		}
		current[key] = value
		managed[key] = true
	}

	managedKeys := make([]string, 0, len(managed))
	for k := range managed {
		managedKeys = append(managedKeys, k)
	}
	sort.Strings(managedKeys)
	setManagedList(current, managedKeys)

	return writeSettings(target, current, opts)
}

// clearSettingsManaged is the no-managed-content path: drop previously-
// managed keys (and the marker) but leave the rest of the file intact.
// If no settings.json exists or there's nothing to drop, no-op.
func clearSettingsManaged(roots scopeRoots, opts Options) error {
	target := settingsPath(roots)
	current, err := readSettings(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	prevManaged := readManagedList(current)
	if len(prevManaged) == 0 {
		return nil
	}
	for _, k := range prevManaged {
		delete(current, k)
	}
	clearManagedMarker(current)
	return writeSettings(target, current, opts)
}

func readSettings(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- reads the settings.json agtk merges into, under the scope root
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("claude: read %s: %w", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("claude: parse %s: %w", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func writeSettings(path string, m map[string]any, opts Options) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301 -- 0755: the .claude directory in the user's repo, meant to be committed
		return fmt.Errorf("claude: mkdir %s: %w", filepath.Dir(path), err)
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("claude: marshal settings: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o644); err != nil { // #nosec G306 -- 0644: settings.json in the user's repo, read by Claude Code itself
		return fmt.Errorf("claude: write %s: %w", path, err)
	}
	if opts.Stdout != nil {
		fmt.Fprintf(opts.Stdout, "wrote %s\n", path)
	}
	return nil
}

// _meta.agtk.managed list helpers.

func readManagedList(m map[string]any) []string {
	meta, ok := m["_meta"].(map[string]any)
	if !ok {
		return nil
	}
	agtk, ok := meta["agtk"].(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := agtk["managed"].([]any)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			keys = append(keys, s)
		}
	}
	return keys
}

func setManagedList(m map[string]any, keys []string) {
	meta, _ := m["_meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	agtk, _ := meta["agtk"].(map[string]any)
	if agtk == nil {
		agtk = map[string]any{}
	}
	asAny := make([]any, len(keys))
	for i, k := range keys {
		asAny[i] = k
	}
	agtk["managed"] = asAny
	meta["agtk"] = agtk
	m["_meta"] = meta
}

func clearManagedMarker(m map[string]any) {
	meta, ok := m["_meta"].(map[string]any)
	if !ok {
		return
	}
	delete(meta, "agtk")
	if len(meta) == 0 {
		delete(m, "_meta")
	} else {
		m["_meta"] = meta
	}
}

// ===== per-category collection =====

// collectHooks builds the Claude `hooks` block: events keyed by name,
// each holding a list of `{ matcher, hooks: [...] }` entries.
func collectHooks(plan *resolver.Plan) map[string]any {
	type matcherBlock struct {
		Matcher string           `json:"matcher,omitempty"`
		Hooks   []map[string]any `json:"hooks"`
	}
	byEvent := map[string][]matcherBlock{}
	for _, d := range plan.Definitions {
		if d.Category != definitions.CategoryHook {
			continue
		}
		h := d.Definition.(*definitions.Hook)
		hookEntry := map[string]any{}
		switch h.Handler.Type {
		case definitions.HandlerCommand:
			hookEntry["type"] = "command"
			hookEntry["command"] = h.Handler.Command
		case definitions.HandlerPrompt:
			hookEntry["type"] = "prompt"
			hookEntry["prompt"] = h.Handler.Prompt
			if h.Handler.Model != "" {
				hookEntry["model"] = h.Handler.Model
			}
		}
		if h.Timeout > 0 {
			hookEntry["timeout"] = h.Timeout
		}
		byEvent[h.Event] = append(byEvent[h.Event], matcherBlock{
			Matcher: h.Matcher,
			Hooks:   []map[string]any{hookEntry},
		})
	}
	if len(byEvent) == 0 {
		return nil
	}
	out := map[string]any{}
	events := make([]string, 0, len(byEvent))
	for e := range byEvent {
		events = append(events, e)
	}
	sort.Strings(events)
	for _, e := range events {
		blocks := byEvent[e]
		converted := make([]any, len(blocks))
		for i, b := range blocks {
			entry := map[string]any{}
			if b.Matcher != "" {
				entry["matcher"] = b.Matcher
			}
			entry["hooks"] = b.Hooks
			converted[i] = entry
		}
		out[e] = converted
	}
	return out
}

// addMemoryGrants appends the store-path permissions, built from the memory
// root this consumer actually resolves to.
//
// They cannot be written into a settings definition. A definition is shared,
// and `memory.root` is honoured only in the consumer's own entry manifest, so
// a literal in the definition is correct for consumers who left the default
// and wrong for every consumer who did not — a permission prompt on every
// delegation, for a path agtk itself chose to move. The value a definition
// carries is opaque to the merge, so nothing along that route can substitute
// the root either.
//
// Only appended when some definition already contributes `permissions`. A
// consumer whose stack pre-approves nothing has said what it wants, and
// conjuring the key here would hand it grants it never asked for.
func addMemoryGrants(fragments map[string]any, plan *resolver.Plan) error {
	perms, ok := fragments[permissionsKey].(map[string]any)
	if !ok {
		return nil
	}
	existing, present := perms[allowKey]
	if !present {
		// Gated on the allow list, not on the permissions key. A deny-only
		// contribution is a stack restricting what an agent may do, and
		// growing it an allow list it never wrote would answer a tightening
		// with a grant.
		return nil
	}
	allow, isList := existing.([]any)
	if !isList {
		// Silently dropping the grants here would leave the explorer
		// prompting on a store agtk located itself, with nothing said.
		return fmt.Errorf("claude: settings `%s.%s` is %T, want a list", permissionsKey, allowKey, existing)
	}
	if !usesMemoryStore(plan) {
		return nil
	}

	root := ""
	if plan.Stack != nil {
		root = plan.Stack.MemoryRoot()
	}
	// Checked here as well as in the memory commands: this is a second entry
	// point to the same field, and a root the rest of agtk refuses would
	// otherwise render into patterns that can match no store — a prompt on
	// every delegation with no diagnostic anywhere.
	if err := memory.ValidateRoot(root); err != nil {
		return fmt.Errorf("claude: %w", err)
	}
	if root == "" {
		root = memory.DefaultRoot
	}
	for _, grant := range memoryGrants(root) {
		if !containsGrant(allow, grant) {
			allow = append(allow, grant)
		}
	}
	perms[allowKey] = allow
	return nil
}

// usesMemoryStore reports whether this consumer has adopted the memory store,
// from either kind of positive evidence: a `memory:` block in its entry
// manifest, or a definition in the plan that reads the store.
//
// Presence of `memory:` alone is not enough to ask, because the consumer that
// adopts memory and leaves `memory.root` at the default writes no block at
// all — which is the common case these grants exist for. A stack that ships
// no memory tooling and pre-approves something unrelated is the case that
// must not pick them up: the append happens after the last-wins merge, so
// such a consumer could not take the key back, and the only way left to
// decline would be a deny rule saying something else.
//
// The agent is named here, so renaming it silently stops the grants. The
// default-stack render test asserts they arrive, which is what fails if it is
// ever renamed without this.
func usesMemoryStore(plan *resolver.Plan) bool {
	if plan.Stack != nil && plan.Stack.Memory != nil {
		return true
	}
	for _, d := range plan.Definitions {
		if d.Category == definitions.CategoryAgent && d.Name == memoryExplorerAgent {
			return true
		}
	}
	return false
}

// memoryGrants renders the two store paths as permission patterns.
//
// A named root is matched under any prefix, so the grant holds whether the
// consumer is rendered at the repo root or under a nested working directory.
// `memory.root: .` has no directory to name, and reusing the same shape there
// would produce `Write(**/candidates/**)` — a write grant on every directory
// called `candidates` anywhere in the tree, which is far more than the
// consumer asked for. That case is anchored instead: the store is the project
// root, so the paths are exactly these two.
func memoryGrants(root string) []string {
	cleaned := path.Clean(filepath.ToSlash(root))
	if cleaned == "." {
		return []string{
			"Read(" + memory.IndexFile + ")",
			"Write(" + memory.CandidatesDir + "/**)",
		}
	}
	return []string{
		"Read(**/" + cleaned + "/" + memory.IndexFile + ")",
		"Write(**/" + cleaned + "/" + memory.CandidatesDir + "/**)",
	}
}

func containsGrant(allow []any, grant string) bool {
	for _, a := range allow {
		if s, ok := a.(string); ok && s == grant {
			return true
		}
	}
	return false
}

// collectSettingFragments returns the union of every setting
// definition's value, with last-stack-wins resolution at the top-level
// key. Stack order comes from plan.StackOrder (depth-first post-order;
// later index = applied later = wins). Definitions in the plan are
// sorted by name (not by stack), so we re-derive ordering here.
func collectSettingFragments(plan *resolver.Plan) map[string]any {
	type contribution struct {
		StackIdx int
		DefName  string
		Value    map[string]any
	}
	stackIdx := map[string]int{}
	for i, id := range plan.StackOrder {
		stackIdx[id] = i
	}
	var contribs []contribution
	for _, d := range plan.Definitions {
		if d.Category != definitions.CategorySetting {
			continue
		}
		s := d.Definition.(*definitions.Setting)
		idx, ok := stackIdx[d.StackName]
		if !ok {
			idx = -1
		}
		contribs = append(contribs, contribution{StackIdx: idx, DefName: d.Name, Value: s.Value})
	}
	if len(contribs) == 0 {
		return nil
	}
	// Stable sort: lowest stack index first, name as tiebreak. Later
	// contributions overwrite earlier at the top-level key.
	sort.SliceStable(contribs, func(i, j int) bool {
		if contribs[i].StackIdx != contribs[j].StackIdx {
			return contribs[i].StackIdx < contribs[j].StackIdx
		}
		return contribs[i].DefName < contribs[j].DefName
	})
	out := map[string]any{}
	for _, c := range contribs {
		for k, v := range c.Value {
			out[k] = v
		}
	}
	return out
}
