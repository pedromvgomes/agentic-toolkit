package claude

import (
	"path/filepath"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// mcpJSONPath returns the target path for .mcp.json under project scope.
// Claude Code only discovers project-scoped MCP servers from this file
// at the project root — not from settings.json's `mcpServers` key, which
// is not a location Claude Code reads for MCP configuration.
func mcpJSONPath(roots scopeRoots) string {
	return filepath.Join(roots.ProjectRoot, ".mcp.json")
}

// renderMCP writes .mcp.json's `mcpServers` key, mirroring settings.go's
// mixed-ownership approach: ownership of the key is recorded in
// `_meta.agtk.managed` so re-renders drop servers removed from the plan
// while preserving anything else already in the file.
//
// User scope is intentionally a no-op: Claude Code stores user-scoped
// MCP servers in ~/.claude.json under a per-project "projects" map, a
// different shape from .mcp.json's flat `mcpServers` object, and that
// mapping isn't implemented yet.
func renderMCP(plan *resolver.Plan, roots scopeRoots, opts Options) error {
	if roots.Scope != ScopeProject {
		return nil
	}

	mcps := collectMCPs(plan)
	if len(mcps) == 0 {
		return clearMCPManaged(roots, opts)
	}

	target := mcpJSONPath(roots)
	current, err := readSettings(target)
	if err != nil {
		return err
	}

	prevManaged := readManagedList(current)
	for _, k := range prevManaged {
		delete(current, k)
	}

	current["mcpServers"] = mcps
	setManagedList(current, []string{"mcpServers"})

	return writeSettings(target, current, opts)
}

// clearMCPManaged is the no-MCP-servers path: drop the previously-
// managed `mcpServers` key (and the marker) but leave the rest of the
// file intact. If .mcp.json doesn't exist or there's nothing to drop,
// no-op.
func clearMCPManaged(roots scopeRoots, opts Options) error {
	target := mcpJSONPath(roots)
	current, err := readSettings(target)
	if err != nil {
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
