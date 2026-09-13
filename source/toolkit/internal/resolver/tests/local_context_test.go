package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/claude"
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
)

// rawContextBody is what a consumer's own top-level prose looks like: no
// frontmatter, and content a frontmatter split would mangle.
const rawContextBody = "# This repo\n\nPlain prose, no frontmatter.\n\n---\n\nA horizontal rule, not a delimiter.\n"

func TestResolve_LocalContext_BecomesInstructionNamedContext(t *testing.T) {
	plan, err := resolveLocal(t, makeMapFS(map[string]string{
		".agentic-toolkit.yaml": localBody(map[string]string{"context": "./CONTEXT.md"}),
		"CONTEXT.md":            rawContextBody,
	}))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %+v, want 1", plan.Definitions)
	}
	d := plan.Definitions[0]
	if d.Category != definitions.CategoryInstruction || d.Name != "context" {
		t.Errorf("got (%s, %q), want (instruction, \"context\")", d.Category, d.Name)
	}
	if d.StackName != "local.context" {
		t.Errorf("stack name = %q, want local.context", d.StackName)
	}
	if last := plan.StackOrder[len(plan.StackOrder)-1]; last != "local.context" {
		t.Errorf("StackOrder = %v, want local.context last", plan.StackOrder)
	}
	inst, ok := d.Definition.(*definitions.Instruction)
	if !ok {
		t.Fatalf("definition = %T, want *definitions.Instruction", d.Definition)
	}
	if inst.Body != rawContextBody {
		t.Errorf("body = %q, want the file read verbatim (%q)", inst.Body, rawContextBody)
	}
}

// The name is fixed, so the file the consumer points at can be called
// anything and moved anywhere without the rendered output changing.
func TestResolve_LocalContext_NameIsFixedRegardlessOfFilename(t *testing.T) {
	plan, err := resolveLocal(t, makeMapFS(map[string]string{
		".agentic-toolkit.yaml": localBody(map[string]string{"context": "./docs/house-style.md"}),
		"docs/house-style.md":   rawContextBody,
	}))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Name != "context" {
		t.Fatalf("definitions = %+v, want one named context", plan.Definitions)
	}
}

func TestResolve_LocalContext_RendersVerbatimIntoManagedRegion(t *testing.T) {
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": localBody(map[string]string{"context": "./CONTEXT.md"}),
		"CONTEXT.md":            rawContextBody,
	})
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}
	plan, err := resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", newFakeProvider())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	tmp := t.TempDir()
	if err := claude.Render(plan, claude.Options{
		Scope: claude.ScopeProject, ScopeRoot: filepath.Join(tmp, ".claude"), ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("render: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(tmp, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), strings.TrimSpace(rawContextBody)) {
		t.Errorf("CLAUDE.md = %q, want it to carry the context file verbatim", raw)
	}
}

// ===== refusals =====

func TestResolve_LocalContext_MissingFileRefused(t *testing.T) {
	_, err := resolveLocal(t, makeMapFS(map[string]string{
		".agentic-toolkit.yaml": localBody(map[string]string{"context": "./CONTEXT.md"}),
	}))
	if err == nil {
		t.Fatal("resolve: want error for a local.context file that does not exist")
	}
	for _, want := range []string{"local.context", "CONTEXT.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to name %s", err, want)
		}
	}
}

func TestResolve_LocalContext_ReportedAlongsideBadDirectory(t *testing.T) {
	_, err := resolveLocal(t, makeMapFS(map[string]string{
		".agentic-toolkit.yaml": localBody(map[string]string{
			"context": "./CONTEXT.md",
			"skills":  "./mine/skills",
		}),
	}))
	if err == nil {
		t.Fatal("resolve: want error")
	}
	for _, want := range []string{"local.context", "local.skills"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to name %s", err, want)
		}
	}
}

// ===== local wins =====

// The fixed name collides with any catalog instruction called "context", and
// the local one wins: it is the consumer's own.
func TestResolve_LocalContext_OverridesExtendsInstruction(t *testing.T) {
	provider := extendsProvider(map[string]string{
		"stacks/base.yaml": stackBody(nil, map[string][]string{
			"instructions": {"context"},
		}),
		"definitions/instructions/context.md": validInstructionBody("Upstream context"),
	})
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": stackBody([]string{"github.com/upstream/base.git/stacks/base.yaml@main"}, nil) +
			localBody(map[string]string{"context": "./CONTEXT.md"}),
		"CONTEXT.md": rawContextBody,
	})
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}
	plan, err := resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", provider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %+v, want 1", plan.Definitions)
	}
	if got := plan.Definitions[0].StackName; got != "local.context" {
		t.Errorf("winner stack = %q, want local.context", got)
	}
	var overrides []string
	for _, d := range plan.Diagnostics {
		if d.Kind == resolver.DiagOverride {
			overrides = append(overrides, d.Message)
		}
	}
	if len(overrides) != 1 || !strings.Contains(overrides[0], `"local.context"`) {
		t.Errorf("override diagnostics = %v, want one naming local.context", overrides)
	}
}
