package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
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
// Conflict resolution between hook/mcp/setting contributions to the
// same top-level key:
//   - Hook definitions own `hooks` exclusively. If a setting tries to
//     write `hooks`, the hook definitions win (the setting's `hooks`
//     fragment is dropped silently).
//   - MCP definitions own `mcpServers` exclusively. Same precedence.
//   - For other top-level keys touched by multiple settings, last by
//     preset stack order wins (preset index lookup against
//     plan.Config.Presets), with stable PlannedDefinition order as
//     tiebreak.
func renderSettings(plan *resolver.Plan, roots scopeRoots, opts Options) error {
	hooks := collectHooks(plan)
	mcps := collectMCPs(plan)
	settingFragments := collectSettingFragments(plan)

	if len(hooks) == 0 && len(mcps) == 0 && len(settingFragments) == 0 {
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
	if len(mcps) > 0 {
		current["mcpServers"] = mcps
		managed["mcpServers"] = true
	}
	for key, value := range settingFragments {
		if managed[key] {
			// hook/mcp already claimed this key; setting contribution
			// is suppressed for safety.
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
	raw, err := os.ReadFile(path)
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("claude: mkdir %s: %w", filepath.Dir(path), err)
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("claude: marshal settings: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o644); err != nil {
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

// collectMCPs builds the Claude `mcpServers` block keyed by definition
// name, with shape derived from transport.
func collectMCPs(plan *resolver.Plan) map[string]any {
	out := map[string]any{}
	for _, d := range plan.Definitions {
		if d.Category != definitions.CategoryMCP {
			continue
		}
		m := d.Definition.(*definitions.MCPServer)
		entry := map[string]any{}
		switch m.Transport {
		case definitions.TransportStdio:
			entry["command"] = m.Command
			if len(m.Args) > 0 {
				entry["args"] = m.Args
			}
			if len(m.Env) > 0 {
				entry["env"] = m.Env
			}
		case definitions.TransportHTTP, definitions.TransportSSE:
			entry["type"] = string(m.Transport)
			entry["url"] = m.URL
			if len(m.Headers) > 0 {
				entry["headers"] = m.Headers
			}
		}
		out[m.Name] = entry
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
