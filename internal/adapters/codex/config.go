package codex

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// configPath returns the target path for the project-scoped config.toml.
func configPath(rts roots) string {
	return filepath.Join(rts.ProjectRoot, ".codex", "config.toml")
}

// hooksFeaturePath is the dotted path of the flag that turns hook
// execution on. Codex reads no hook unless it is set, so agtk owns it
// alongside the hook tables themselves rather than asking a consumer to
// remember it.
const hooksFeaturePath = "features.hooks"

// renderConfig writes the mcp, setting, and hook categories into
// .codex/config.toml. Codex has a single config surface where Claude has
// two (settings.json and .mcp.json), so all three land in one file.
//
// Ownership is mixed, mirroring the Claude adapter's settings.json model:
// `_meta.agtk.managed` records the dotted key paths agtk wrote, every
// render drops exactly those and rewrites them, and anything else in the
// file is preserved. Paths are dotted rather than top-level-only because
// the hooks feature flag is one key inside a `[features]` table a
// consumer may also be using for their own flags — dropping the whole
// table to reclaim one key would take their flags with it.
//
// A hand-edited config.toml survives a re-render as data, not as text:
// the file is decoded, merged, and re-encoded, so comments and key
// ordering in untouched parts are not preserved.
//
// Conflict resolution between categories contributing the same path:
// mcp definitions own `mcp_servers`, hook definitions own `hooks` and
// the feature flag, and a setting fragment naming an already-claimed
// path is dropped — the same precedence settings.json gives hooks over
// settings. Claims are compared segment-wise, because writing a table
// claims everything under it: a fragment naming `features` would
// otherwise pass a string-equality check against `features.hooks` and
// replace the table the flag lives in, leaving every rendered hook in
// the file and none of them running.
func renderConfig(plan *resolver.Plan, rts roots, opts Options) error {
	mcpServers, mcpNotes := collectMCPServers(plan)
	hooks, hookNotes := collectHooks(plan)
	settingFragments := collectSettingFragments(plan)

	reportNotes(opts.Stdout, mcpNotes)
	reportNotes(opts.Stdout, hookNotes)

	target := configPath(rts)

	if len(mcpServers) == 0 && len(hooks) == 0 && len(settingFragments) == 0 {
		return clearConfigManaged(target, opts)
	}

	current, err := readConfig(target)
	if err != nil {
		return err
	}

	// Drop every previously-managed path so a definition removed between
	// renders doesn't leave its key behind.
	for _, p := range readManagedList(current) {
		deletePath(current, p)
	}

	// claimed accumulates the paths agtk writes, in claim order, and is
	// what the next render reads back to release them. A slice rather
	// than a set because overlap is checked against whole segments, not
	// by key equality: a path is refused when an existing claim names a
	// table it lives inside, which no map lookup answers.
	var claimed []string
	claim := func(path string, value any) {
		for _, c := range claimed {
			if pathsOverlap(c, path) {
				return
			}
		}
		setPath(current, path, value)
		claimed = append(claimed, path)
	}

	if len(mcpServers) > 0 {
		claim("mcp_servers", mcpServers)
	}
	if len(hooks) > 0 {
		claim("hooks", hooks)
		claim(hooksFeaturePath, true)
	}
	for key, value := range settingFragments {
		claim(key, value)
	}

	sort.Strings(claimed)
	setManagedList(current, claimed)

	return writeConfig(target, current, opts)
}

// clearConfigManaged is the nothing-to-render path: drop previously-
// managed paths (and the marker) but leave the rest of the file intact.
// A config.toml that doesn't exist, or holds nothing of agtk's, is left
// alone.
func clearConfigManaged(target string, opts Options) error {
	current, err := readConfig(target)
	if err != nil {
		return err
	}
	prevManaged := readManagedList(current)
	if len(prevManaged) == 0 {
		return nil
	}
	for _, p := range prevManaged {
		deletePath(current, p)
	}
	clearManagedMarker(current)
	if len(current) == 0 {
		// Everything in the file was agtk's, and there is nothing left to
		// put back. An empty config.toml teaches Codex nothing and shows
		// up in every diff, so the file goes with the last key it held.
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("codex: remove %s: %w", target, err)
		}
		if opts.Stdout != nil {
			fmt.Fprintf(opts.Stdout, "removed %s\n", target)
		}
		return nil
	}
	return writeConfig(target, current, opts)
}

func readConfig(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- reads the config.toml agtk merges into, under the project's .codex dir
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("codex: read %s: %w", path, err)
	}
	var m map[string]any
	if err := toml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("codex: parse %s: %w", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func writeConfig(path string, m map[string]any, opts Options) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301 -- 0755: the .codex directory in the user's repo, meant to be committed
		return fmt.Errorf("codex: mkdir %s: %w", filepath.Dir(path), err)
	}
	raw, err := toml.Marshal(m)
	if err != nil {
		return fmt.Errorf("codex: marshal config: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil { // #nosec G306 -- 0644: config.toml in the user's repo, read by Codex itself
		return fmt.Errorf("codex: write %s: %w", path, err)
	}
	if opts.Stdout != nil {
		fmt.Fprintf(opts.Stdout, "wrote %s\n", path)
	}
	return nil
}

// reportNotes prints one line per category that had something to say
// about content it did not render.
func reportNotes(stdout io.Writer, notes []string) {
	if stdout == nil {
		return
	}
	for _, n := range notes {
		fmt.Fprintf(stdout, "%s\n", n)
	}
}

// ===== dotted-path access =====

// splitPath splits a dotted managed-key path into its segments.
func splitPath(p string) []string { return strings.Split(p, ".") }

// pathsOverlap reports whether writing one of these paths would disturb
// the other: the same path, or one naming a table the other lives
// inside. Comparison is segment-wise, so `features` overlaps
// `features.hooks` while `feature_flags` overlaps neither.
func pathsOverlap(a, b string) bool {
	return a == b ||
		strings.HasPrefix(a, b+".") ||
		strings.HasPrefix(b, a+".")
}

// setPath writes value at the dotted path, creating intermediate tables.
// A non-table value blocking the way is replaced: agtk claimed the path,
// so it owns every level of it.
func setPath(m map[string]any, p string, value any) {
	segs := splitPath(p)
	cur := m
	for _, seg := range segs[:len(segs)-1] {
		next, ok := cur[seg].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[seg] = next
		}
		cur = next
	}
	cur[segs[len(segs)-1]] = value
}

// deletePath removes the dotted path, pruning any intermediate table it
// leaves empty so reclaiming one key out of a `[features]` table doesn't
// strand an empty table behind.
func deletePath(m map[string]any, p string) {
	segs := splitPath(p)
	if len(segs) == 1 {
		delete(m, segs[0])
		return
	}
	child, ok := m[segs[0]].(map[string]any)
	if !ok {
		return
	}
	deletePath(child, strings.Join(segs[1:], "."))
	if len(child) == 0 {
		delete(m, segs[0])
	}
}

// ===== _meta.agtk.managed list helpers =====

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
	paths := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			paths = append(paths, s)
		}
	}
	return paths
}

func setManagedList(m map[string]any, paths []string) {
	meta, _ := m["_meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	agtk, _ := meta["agtk"].(map[string]any)
	if agtk == nil {
		agtk = map[string]any{}
	}
	asAny := make([]any, len(paths))
	for i, p := range paths {
		asAny[i] = p
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

// collectMCPServers builds the `mcp_servers` table keyed by definition
// name. Transport picks the shape: a stdio server is a command plus its
// argv and environment, a streamable-http server is a url. Codex has no
// sse client, so an sse definition is reported and skipped rather than
// written as a url Codex would fail to connect to.
//
// Canonical OAuthConfig has no destination here: Codex authenticates a
// remote server through `auth`/`bearer_token_env_var`, not through a
// client-id-and-scopes block.
func collectMCPServers(plan *resolver.Plan) (map[string]any, []string) {
	out := map[string]any{}
	var notes []string
	for _, d := range plan.Definitions {
		if d.Category != definitions.CategoryMCP {
			continue
		}
		m := d.Definition.(*definitions.MCPServer)
		// A Codex args override replaces the canonical argv rather than
		// extending it: the difference it exists for is one flag's value,
		// and appending would leave both values on the command line.
		args := m.Args
		if ext := m.Extensions.Codex; ext != nil && len(ext.Args) > 0 {
			args = ext.Args
		}
		entry := map[string]any{}
		switch m.Transport {
		case definitions.TransportStdio:
			entry["command"] = m.Command
			if len(args) > 0 {
				entry["args"] = args
			}
			if len(m.Env) > 0 {
				entry["env"] = m.Env
			}
		case definitions.TransportHTTP:
			entry["url"] = m.URL
			if len(m.Headers) > 0 {
				entry["http_headers"] = m.Headers
			}
		case definitions.TransportSSE:
			notes = append(notes, fmt.Sprintf("codex: mcp %q not rendered: sse transport has no Codex equivalent (Codex speaks stdio and streamable http)", m.Name))
			continue
		}
		if ext := m.Extensions.Codex; ext != nil {
			if len(ext.EnabledTools) > 0 {
				entry["enabled_tools"] = ext.EnabledTools
			}
			if len(ext.DisabledTools) > 0 {
				entry["disabled_tools"] = ext.DisabledTools
			}
			if ext.ApprovalMode != "" {
				entry["default_tools_approval_mode"] = ext.ApprovalMode
			}
			if ext.StartupTimeoutSec > 0 {
				// Codex reads this as a fractional number of seconds; a
				// TOML integer is a different type and is rejected.
				entry["startup_timeout_sec"] = float64(ext.StartupTimeoutSec)
			}
			if ext.Required {
				entry["required"] = true
			}
			if ext.BearerTokenEnvVar != "" {
				entry["bearer_token_env_var"] = ext.BearerTokenEnvVar
			}
		}
		out[m.Name] = entry
	}
	if len(out) == 0 {
		return nil, notes
	}
	return out, notes
}

// collectHooks builds the `hooks` table: one array-of-tables per event,
// each entry a matcher plus its own `hooks` array of handlers.
//
// Only command handlers are rendered. Codex executes command and
// mcp_tool handlers, and mcp_tool is not a canonical HandlerType — it
// exists only as a Claude extension, which this adapter does not read.
// A prompt handler is reported and skipped: Codex parses one but never
// runs it, so writing it would put a hook in the config that silently
// never fires.
//
// Canonical FailClosed has no destination either. A Codex hook blocks by
// what it exits with, not by a key in the config, so there is nothing to
// write it to.
func collectHooks(plan *resolver.Plan) (map[string]any, []string) {
	byEvent := map[string][]any{}
	var notes []string
	for _, d := range plan.Definitions {
		if d.Category != definitions.CategoryHook {
			continue
		}
		h := d.Definition.(*definitions.Hook)
		handler := map[string]any{}
		switch h.Handler.Type {
		case definitions.HandlerCommand:
			handler["type"] = "command"
			handler["command"] = h.Handler.Command
		case definitions.HandlerPrompt:
			notes = append(notes, fmt.Sprintf("codex: hook %q not rendered: Codex parses a prompt handler but never runs it (it executes command and mcp_tool handlers only)", h.Name))
			continue
		default:
			continue
		}
		if h.Timeout > 0 {
			handler["timeout"] = h.Timeout
		}
		if ext := h.Extensions.Codex; ext != nil && ext.Async {
			handler["async"] = true
		}
		block := map[string]any{"hooks": []any{handler}}
		if h.Matcher != "" {
			block["matcher"] = h.Matcher
		}
		byEvent[h.Event] = append(byEvent[h.Event], block)
	}
	if len(byEvent) == 0 {
		return nil, notes
	}
	out := map[string]any{}
	for event, blocks := range byEvent {
		out[event] = blocks
	}
	return out, notes
}

// collectSettingFragments returns the union of every setting
// definition's value, with last-stack-wins resolution at the top-level
// key, matching the Claude adapter's resolution exactly: stack order
// comes from plan.StackOrder (later index = applied later = wins), with
// definition name as the tiebreak.
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
