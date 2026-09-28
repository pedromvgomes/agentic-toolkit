package codex

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// orderInstructions places the entry manifest's `context:` instruction
// first, then everything a stack declared — unchanged from the order
// plan.Definitions already carries — then locally scanned instructions
// last, sorted by EntryPath. A scanned file's filename decides its
// position, not its declared `name:`, since the filesystem scan that
// found it is itself lexicographic.
func orderInstructions(defs []resolver.PlannedDefinition) []*definitions.Instruction {
	var context *definitions.Instruction
	var declared []*definitions.Instruction
	var scanned []resolver.PlannedDefinition

	for _, d := range defs {
		switch {
		case d.IsContext:
			context = d.Definition.(*definitions.Instruction)
		case d.Scanned:
			scanned = append(scanned, d)
		default:
			declared = append(declared, d.Definition.(*definitions.Instruction))
		}
	}

	sort.Slice(scanned, func(i, j int) bool { return scanned[i].EntryPath < scanned[j].EntryPath })

	out := make([]*definitions.Instruction, 0, len(defs))
	if context != nil {
		out = append(out, context)
	}
	out = append(out, declared...)
	for _, d := range scanned {
		out = append(out, d.Definition.(*definitions.Instruction))
	}
	return out
}

// buildAgentsMD renders AGENTS.md's content: the instruction bodies, in
// orderInstructions order (context first, then stack-declared, then locally
// scanned by filename), followed by an index of rules (relative link to its
// whole-owned file + ruleSummary) sorted by name for a stable diff. Codex
// has no rules-discovery mechanism of its own, so this index is how a rule
// is ever found.
func buildAgentsMD(instructions []resolver.PlannedDefinition, rules []*definitions.Rule) []byte {
	var b strings.Builder
	first := true
	for _, inst := range orderInstructions(instructions) {
		body := strings.TrimSpace(inst.Body)
		if body == "" {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		first = false
		b.WriteString(body)
		b.WriteString("\n")
	}

	if len(rules) > 0 {
		sorted := make([]*definitions.Rule, len(rules))
		copy(sorted, rules)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

		if !first {
			b.WriteString("\n")
		}
		b.WriteString("## Rules\n\n")
		for _, r := range sorted {
			if summary := ruleSummary(r); summary != "" {
				fmt.Fprintf(&b, "- [%s](.agents/rules/%s.md): %s\n", r.Name, r.Name, summary)
			} else {
				fmt.Fprintf(&b, "- [%s](.agents/rules/%s.md)\n", r.Name, r.Name)
			}
		}
	}

	return []byte(b.String())
}

// ruleSummary is the text after a rule's index link: its description, else
// the first `# ` heading of its body, else nothing. A rule file carrying no
// description frontmatter otherwise renders as a link followed by a dangling
// `: `, which tells a reader the summary is missing rather than that the
// heading already says what the rule is.
func ruleSummary(r *definitions.Rule) string {
	if d := strings.TrimSpace(r.Description); d != "" {
		return d
	}
	for _, line := range strings.Split(r.Body, "\n") {
		if heading, ok := strings.CutPrefix(strings.TrimSpace(line), "# "); ok {
			return strings.TrimSpace(heading)
		}
	}
	return ""
}
