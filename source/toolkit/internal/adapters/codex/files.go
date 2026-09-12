package codex

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/fsops"
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

var wholeOps = fsops.New("codex")

// planWholeOwned builds every whole-owned WholeOp for plan: a bundle per
// skill, a bundle per command converted to a skill, a file per rule
// (plus its AGENTS.md index entry), a TOML file per subagent, and — if
// there is at least one instruction or rule to say something — the
// synthesized AGENTS.md itself. All RelPaths are relative to
// rts.ProjectRoot, per this package's doc comment.
//
// A skill and a command sharing a name resolve onto the same
// .agents/skills/<name>/SKILL.md. The authored skill wins and the
// converted command is not written, matching what Claude Code does with
// the same collision rather than inventing a second rule for it. The
// skipped command is reported on stdout, not raised as an error: a
// collision leaves both platforms with a working render, since the
// command still renders as a command on Claude.
func planWholeOwned(plan *resolver.Plan, rts roots, stdout io.Writer) ([]fsops.WholeOp, error) {
	var ops []fsops.WholeOp
	var instructions []*definitions.Instruction
	var rules []*definitions.Rule
	var notes []string

	skillNames := map[string]bool{}
	for _, d := range plan.Definitions {
		if d.Category == definitions.CategorySkill {
			skillNames[d.Name] = true
		}
	}

	for _, d := range plan.Definitions {
		switch d.Category {
		case definitions.CategorySkill:
			built, err := wholeOps.BuildBundleOps(d, rts.ProjectRoot, ".agents/skills", "SKILL.md", renderSkill)
			if err != nil {
				return nil, err
			}
			ops = append(ops, built...)
		case definitions.CategoryRule:
			r := d.Definition.(*definitions.Rule)
			rules = append(rules, r)
			content, err := renderRule(d.Definition)
			if err != nil {
				return nil, err
			}
			ops = append(ops, fsops.SingleFileOp(rts.ProjectRoot, ".agents/rules", d.Name+".md", content))
		case definitions.CategoryCommand:
			if skillNames[d.Name] {
				notes = append(notes, fmt.Sprintf("codex: command %q not rendered: a skill of the same name owns .agents/skills/%s/SKILL.md", d.Name, d.Name))
				continue
			}
			// A command is a single file, not a bundle directory, so
			// there are no companion files to carry across — only the
			// converted SKILL.md the skill directory is created for.
			content, err := renderCommandAsSkill(d.Definition)
			if err != nil {
				return nil, err
			}
			ops = append(ops, fsops.SingleFileOp(rts.ProjectRoot, ".agents/skills/"+d.Name, "SKILL.md", content))
		case definitions.CategoryAgent:
			content, err := renderAgent(d.Definition)
			if err != nil {
				return nil, err
			}
			ops = append(ops, fsops.SingleFileOp(rts.ProjectRoot, ".codex/agents", d.Name+".toml", content))
		case definitions.CategoryInstruction:
			instructions = append(instructions, d.Definition.(*definitions.Instruction))
		}
	}

	if len(instructions) > 0 || len(rules) > 0 {
		ops = append(ops, fsops.SingleFileOp(rts.ProjectRoot, "", "AGENTS.md", buildAgentsMD(instructions, rules)))
	}

	reportNotes(stdout, notes)

	sort.Slice(ops, func(i, j int) bool { return ops[i].RelPath < ops[j].RelPath })
	return ops, nil
}

// renderSkill renders a skill's SKILL.md. Codex's documented frontmatter
// shape is just name and description — no allowed-tools or argument-hint
// vocabulary the way Claude's skills have.
func renderSkill(def definitions.Definition) ([]byte, error) {
	s := def.(*definitions.Skill)
	type fm struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	return frontmatterPlusBody(fm{Name: s.Name, Description: s.Description}, s.Body)
}

// renderRule renders a rule's whole-owned file under .agents/rules/.
// Codex has no rules-discovery mechanism of its own — buildAgentsMD is
// what makes the file reachable at all.
func renderRule(def definitions.Definition) ([]byte, error) {
	r := def.(*definitions.Rule)
	type fm struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description,omitempty"`
	}
	return frontmatterPlusBody(fm{Name: r.Name, Description: r.Description}, r.Body)
}

func frontmatterPlusBody(frontmatter any, body string) ([]byte, error) {
	yamlBytes, err := yaml.Marshal(frontmatter)
	if err != nil {
		return nil, fmt.Errorf("codex: marshal frontmatter: %w", err)
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
