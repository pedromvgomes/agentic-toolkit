package codex

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// localContextStackName is the stack identifier the resolver gives the
// instruction it reads from the entry manifest's local.context.
const localContextStackName = "local.context"

// buildAgentsMD renders AGENTS.md's content: the instruction bodies, in
// orderInstructions' order, followed by an index of rules (description +
// relative link to its whole-owned file) sorted by name for a stable
// diff. Codex has no rules-discovery mechanism of its own, so this index
// is how a rule is ever found.
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
			fmt.Fprintf(&b, "- [%s](.agents/rules/%s.md): %s\n", r.Name, r.Name, r.Description)
		}
	}

	return []byte(b.String())
}

// orderInstructions arranges the instructions for the single file their
// bodies are concatenated into, in three groups:
//
//  1. local.context, the consumer's own top-level prose, which frames
//     everything the catalog contributes and so comes first.
//  2. everything a stack named, in the order the resolver produced —
//     alphabetical by (category, name).
//  3. the consumer's local.instructions, by ScanOrder, which is the order
//     of their filenames. Those are the consumer's ordering knob; the
//     `name:` each file declares is not.
func orderInstructions(defs []resolver.PlannedDefinition) []*definitions.Instruction {
	var context, named, local []resolver.PlannedDefinition
	for _, d := range defs {
		switch {
		case d.StackName == localContextStackName:
			context = append(context, d)
		case d.ScanOrder > 0:
			local = append(local, d)
		default:
			named = append(named, d)
		}
	}
	sort.SliceStable(local, func(i, j int) bool { return local[i].ScanOrder < local[j].ScanOrder })

	out := make([]*definitions.Instruction, 0, len(defs))
	for _, group := range [][]resolver.PlannedDefinition{context, named, local} {
		for _, d := range group {
			out = append(out, d.Definition.(*definitions.Instruction))
		}
	}
	return out
}
