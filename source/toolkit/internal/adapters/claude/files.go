package claude

import (
	"fmt"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/fsops"
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// wholeOps is this adapter's fsops.Ops, tagging every whole-owned-file
// error with "claude".
var wholeOps = fsops.New("claude")

// planWholeOwned builds the list of every whole-owned-file write the
// plan will perform: skill/agent entry files + their bundle companions,
// command files, rule files. Order is deterministic: alphabetical by
// RelPath.
func planWholeOwned(plan *resolver.Plan, roots scopeRoots) ([]fsops.WholeOp, error) {
	var ops []fsops.WholeOp
	for _, d := range plan.Definitions {
		switch d.Category {
		case definitions.CategorySkill:
			built, err := wholeOps.BuildBundleOps(d, roots.ScopeRoot, "skills", "SKILL.md", renderSkill)
			if err != nil {
				return nil, err
			}
			ops = append(ops, built...)
		case definitions.CategoryAgent:
			built, err := wholeOps.BuildBundleOps(d, roots.ScopeRoot, "agents", "AGENT.md", renderAgent)
			if err != nil {
				return nil, err
			}
			ops = append(ops, built...)
		case definitions.CategoryCommand:
			content, err := renderCommand(d.Definition)
			if err != nil {
				return nil, err
			}
			ops = append(ops, fsops.SingleFileOp(roots.ScopeRoot, "commands", d.Name+".md", content))
		case definitions.CategoryRule:
			content, err := renderRule(d.Definition)
			if err != nil {
				return nil, err
			}
			ops = append(ops, fsops.SingleFileOp(roots.ScopeRoot, "rules", d.Name+".md", content))
		}
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].RelPath < ops[j].RelPath })
	return ops, nil
}

// ===== per-category entry rendering =====

func renderSkill(def definitions.Definition) ([]byte, error) {
	s := def.(*definitions.Skill)
	type fm struct {
		Name                   string   `yaml:"name"`
		Description            string   `yaml:"description"`
		AllowedTools           []string `yaml:"allowed-tools,omitempty"`
		ArgumentHint           string   `yaml:"argument-hint,omitempty"`
		DisableModelInvocation bool     `yaml:"disable-model-invocation,omitempty"`
	}
	out := fm{Name: s.Name, Description: s.Description}
	if s.Extensions.Claude != nil {
		out.AllowedTools = s.Extensions.Claude.AllowedTools
		out.ArgumentHint = s.Extensions.Claude.ArgumentHint
		out.DisableModelInvocation = s.Extensions.Claude.DisableModelInvocation
	}
	return frontmatterPlusBody(out, s.Body)
}

func renderAgent(def definitions.Definition) ([]byte, error) {
	a := def.(*definitions.Agent)
	type fm struct {
		Name            string   `yaml:"name"`
		Description     string   `yaml:"description"`
		Model           string   `yaml:"model,omitempty"`
		Tools           []string `yaml:"tools,omitempty"`
		Color           string   `yaml:"color,omitempty"`
		DisallowedTools []string `yaml:"disallowed-tools,omitempty"`
		PermissionMode  string   `yaml:"permission-mode,omitempty"`
		MaxTurns        int      `yaml:"max-turns,omitempty"`
		Memory          string   `yaml:"memory,omitempty"`
		Background      bool     `yaml:"background,omitempty"`
		Effort          string   `yaml:"effort,omitempty"`
		Isolation       string   `yaml:"isolation,omitempty"`
		InitialPrompt   string   `yaml:"initial-prompt,omitempty"`
	}
	out := fm{
		Name:        a.Name,
		Description: a.Description,
		Model:       a.Model,
		Tools:       a.Tools,
		Color:       string(a.Color),
	}
	if a.Extensions.Claude != nil {
		ext := a.Extensions.Claude
		out.DisallowedTools = ext.DisallowedTools
		out.PermissionMode = ext.PermissionMode
		out.MaxTurns = ext.MaxTurns
		out.Memory = ext.Memory
		out.Background = ext.Background
		out.Effort = ext.Effort
		out.Isolation = ext.Isolation
		out.InitialPrompt = ext.InitialPrompt
	}
	return frontmatterPlusBody(out, a.Body)
}

func renderCommand(def definitions.Definition) ([]byte, error) {
	c := def.(*definitions.Command)
	type fm struct {
		Name         string `yaml:"name"`
		Description  string `yaml:"description"`
		ArgumentHint string `yaml:"argument-hint,omitempty"`
		Model        string `yaml:"model,omitempty"`
		// Claude Code reads a slash command's tool allowlist from
		// `allowed-tools`, the way it reads the hint from `argument-hint`.
		// Under any other key the restriction is not rejected — it is
		// ignored, and the command runs with the session's whole tool set.
		Tools []string `yaml:"allowed-tools,omitempty"`
	}
	out := fm{
		Name:         c.Name,
		Description:  c.Description,
		ArgumentHint: c.ArgumentHint,
		Model:        c.Model,
		Tools:        c.Tools,
	}
	return frontmatterPlusBody(out, c.Body)
}

func renderRule(def definitions.Definition) ([]byte, error) {
	r := def.(*definitions.Rule)
	type fm struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description,omitempty"`
	}
	out := fm{Name: r.Name, Description: r.Description}
	return frontmatterPlusBody(out, r.Body)
}

// frontmatterPlusBody serializes a typed frontmatter struct and appends
// the markdown body. Output ends with a single trailing newline.
func frontmatterPlusBody(frontmatter any, body string) ([]byte, error) {
	yamlBytes, err := yaml.Marshal(frontmatter)
	if err != nil {
		return nil, fmt.Errorf("claude: marshal frontmatter: %w", err)
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.Write(yamlBytes)
	b.WriteString("---\n")
	if body != "" {
		if !strings.HasPrefix(body, "\n") {
			b.WriteString("\n")
		}
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
	}
	return []byte(b.String()), nil
}
