package codex

import (
	"fmt"

	"github.com/pelletier/go-toml/v2"

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/definitions"
)

// agentTOML is the shape Codex reads a subagent definition in. name,
// description, and developer_instructions are the required fields;
// everything else is an optional CodexAgentExt override.
type agentTOML struct {
	Name                  string         `toml:"name"`
	Description           string         `toml:"description"`
	DeveloperInstructions string         `toml:"developer_instructions"`
	Model                 string         `toml:"model,omitempty"`
	ModelReasoningEffort  string         `toml:"model_reasoning_effort,omitempty"`
	SandboxMode           string         `toml:"sandbox_mode,omitempty"`
	MCPServers            []string       `toml:"mcp_servers,omitempty"`
	SkillsConfig          map[string]any `toml:"skills_config,omitempty"`
}

// renderAgent renders a subagent's whole-owned TOML file under
// .codex/agents/. Body is the canonical field every platform's agent
// carries; Codex's own vocabulary for it is developer_instructions.
func renderAgent(def definitions.Definition) ([]byte, error) {
	a := def.(*definitions.Agent)
	out := agentTOML{
		Name:                  a.Name,
		Description:           a.Description,
		DeveloperInstructions: a.Body,
		Model:                 a.Model,
	}
	if a.Extensions.Codex != nil {
		ext := a.Extensions.Codex
		out.ModelReasoningEffort = ext.ModelReasoningEffort
		out.SandboxMode = ext.SandboxMode
		out.MCPServers = ext.MCPServers
		out.SkillsConfig = ext.SkillsConfig
	}
	raw, err := toml.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("codex: marshal agent toml: %w", err)
	}
	return raw, nil
}
