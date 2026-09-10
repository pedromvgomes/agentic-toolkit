package codex

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
)

// buildAgentsMD renders AGENTS.md's content: the instruction bodies, in
// plan order, followed by an index of rules (description + relative
// link to its whole-owned file) sorted by name for a stable diff. Codex
// has no rules-discovery mechanism of its own, so this index is how a
// rule is ever found.
func buildAgentsMD(instructions []*definitions.Instruction, rules []*definitions.Rule) []byte {
	var b strings.Builder
	first := true
	for _, inst := range instructions {
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
			fmt.Fprintf(&b, "- [%s](.agents/rules/%s.md): %s\n", r.Name, r.Name, r.Description)
		}
	}

	return []byte(b.String())
}
