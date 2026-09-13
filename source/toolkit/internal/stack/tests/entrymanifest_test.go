package tests

import (
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
)

func TestParseEntryManifestBytes_AllFields(t *testing.T) {
	body := `description: example consumer
root: ./agentic
context: this repo builds a CLI
platforms:
  - claude
  - codex
memory:
  root: .memory
  agent: claudecode
stacks:
  - github.com/foo/bar.git/stacks/default.yaml@main
  - ./local/stack.yaml
`
	m, err := stack.ParseEntryManifestBytes("t.yaml", []byte(body))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if m.Description != "example consumer" {
		t.Errorf("description = %q", m.Description)
	}
	if m.Context != "this repo builds a CLI" {
		t.Errorf("context = %q", m.Context)
	}
	if m.MemoryRoot() != ".memory" {
		t.Errorf("MemoryRoot() = %q", m.MemoryRoot())
	}
	if m.MemoryAgent() != "claudecode" {
		t.Errorf("MemoryAgent() = %q", m.MemoryAgent())
	}
	got := m.EffectivePlatforms()
	if len(got) != 2 || got[0] != definitions.PlatformClaude || got[1] != definitions.PlatformCodex {
		t.Errorf("EffectivePlatforms() = %v, want [claude codex]", got)
	}
	if len(m.Stacks) != 2 {
		t.Fatalf("stacks = %+v", m.Stacks)
	}
	if m.Stacks[0].Kind != stack.RefURL || m.Stacks[0].URL != "github.com/foo/bar.git/stacks/default.yaml" {
		t.Errorf("stacks[0] = %+v", m.Stacks[0])
	}
	if m.Stacks[1].Kind != stack.RefPath || m.Stacks[1].Path != "local/stack.yaml" {
		t.Errorf("stacks[1] = %+v", m.Stacks[1])
	}
}

func TestParseEntryManifestBytes_DefaultRoot(t *testing.T) {
	m, err := stack.ParseEntryManifestBytes("t.yaml", []byte("description: empty\n"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if m.EffectiveRoot() != stack.DefaultLocalRoot {
		t.Errorf("EffectiveRoot() = %q, want %q", m.EffectiveRoot(), stack.DefaultLocalRoot)
	}
	if stack.DefaultLocalRoot != "agentic" {
		t.Errorf("DefaultLocalRoot = %q, want %q", stack.DefaultLocalRoot, "agentic")
	}
}

func TestParseEntryManifestBytes_UnknownFieldRejected(t *testing.T) {
	body := "description: typo\nunknown_field: true\n"
	_, err := stack.ParseEntryManifestBytes("t.yaml", []byte(body))
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestParseEntryManifestBytes_UnknownPlatformRejected(t *testing.T) {
	body := "description: typo\nplatforms:\n  - cluade\n"
	_, err := stack.ParseEntryManifestBytes("t.yaml", []byte(body))
	if err == nil {
		t.Fatal("expected error for unknown platform")
	}
	if !stack.IsKind(err, stack.ErrUnknownPlatform) {
		t.Errorf("error kind != unknown_platform: %v", err)
	}
}
