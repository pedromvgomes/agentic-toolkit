package tests

import (
	"path/filepath"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/codex"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// TestRender_AgentsMD_InstructionGroupOrder: AGENTS.md lists
// local.context first, then the instructions stacks named in the
// resolver's alphabetical order, then the consumer's local.instructions
// in scan (filename) order — which is not the order their names sort in.
func TestRender_AgentsMD_InstructionGroupOrder(t *testing.T) {
	tmp := t.TempDir()

	// Alphabetical by name, the order the resolver hands the adapter.
	plan := makePlan([]resolver.PlannedDefinition{
		withScanOrder(pdInstruction("aaa-local", "scanned second", "aaa local body", "local.instructions"), 2),
		pdInstruction("context", "the consumer's own prose", "context body", "local.context"),
		pdInstruction("house-style", "a stack's own", "house style body", "default"),
		withScanOrder(pdInstruction("zzz-local", "scanned first", "zzz local body", "local.instructions"), 1),
	}, "default", "local.instructions", "local.context")

	if err := codex.Render(plan, codex.Options{
		Scope:       codex.ScopeProject,
		ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	want := "context body\n\nhouse style body\n\nzzz local body\n\naaa local body\n"
	if got := mustRead(t, filepath.Join(tmp, "AGENTS.md")); got != want {
		t.Errorf("AGENTS.md =\n%q\nwant\n%q", got, want)
	}
}

// withScanOrder marks a definition as one the local scan found, at the
// 1-based position its filename held in the directory listing.
func withScanOrder(d resolver.PlannedDefinition, order int) resolver.PlannedDefinition {
	d.ScanOrder = order
	return d
}
