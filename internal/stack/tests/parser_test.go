package tests

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
)

func TestParseBytes_MinimalEmpty(t *testing.T) {
	// Just a description — no extends, no entries. EffectiveRoot returns
	// the default when root is omitted.
	s, err := stack.ParseBytes("test.yaml", []byte("description: empty\n"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.EffectiveRoot() != stack.DefaultRoot {
		t.Errorf("EffectiveRoot = %q, want %q", s.EffectiveRoot(), stack.DefaultRoot)
	}
}

func TestParseBytes_AllCategoriesAndExtends(t *testing.T) {
	body := `description: example
root: ./agentic
extends:
  - github.com/foo/bar.git/stacks/default.yaml@main
  - ./local/stack.yaml
skills:
  - foo
  - ./team/skill
  - github.com/x/y.git/skills/baz@v1
rules:
  - style
  - ./rules/team.md
instructions:
  - plan-approval
commands:
  - lint
  - git/commit
`
	s, err := stack.ParseBytes("t.yaml", []byte(body))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.Description != "example" {
		t.Errorf("description = %q", s.Description)
	}
	if s.EffectiveRoot() != "agentic" && s.EffectiveRoot() != "./agentic" {
		t.Errorf("root = %q", s.EffectiveRoot())
	}
	if len(s.Extends) != 2 {
		t.Fatalf("extends = %+v", s.Extends)
	}
	if s.Extends[0].Kind != stack.RefURL || s.Extends[0].URL != "github.com/foo/bar.git/stacks/default.yaml" {
		t.Errorf("extends[0] = %+v", s.Extends[0])
	}
	if s.Extends[1].Kind != stack.RefPath {
		t.Errorf("extends[1] should be path, got %v", s.Extends[1].Kind)
	}
	if len(s.Skills) != 3 {
		t.Fatalf("skills = %+v", s.Skills)
	}
	if s.Skills[0].Kind != stack.RefBare || s.Skills[0].Name != "foo" {
		t.Errorf("skills[0] = %+v", s.Skills[0])
	}
	if s.Skills[1].Kind != stack.RefPath {
		t.Errorf("skills[1] should be path, got %v", s.Skills[1].Kind)
	}
	if s.Skills[2].Kind != stack.RefURL || s.Skills[2].Ref != "v1" {
		t.Errorf("skills[2] = %+v", s.Skills[2])
	}
	// Nested command name allowed.
	found := false
	for _, c := range s.Commands {
		if c.Kind == stack.RefBare && c.Name == "git/commit" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected git/commit nested command name; got %+v", s.Commands)
	}
}

func TestParseBytes_LegacyConfig_Rejected(t *testing.T) {
	body := "source: github.com/foo/bar@main\npresets:\n  - default\n"
	_, err := stack.ParseBytes("t.yaml", []byte(body))
	if err == nil {
		t.Fatal("expected error for legacy config")
	}
	if !stack.IsKind(err, stack.ErrLegacyConfig) {
		t.Errorf("error kind != legacy_config: %v", err)
	}
	if !strings.Contains(err.Error(), "MIGRATION") {
		t.Errorf("error should reference migration doc: %v", err)
	}
}

func TestParseBytes_BareNameInExtends_Rejected(t *testing.T) {
	body := "extends:\n  - default\n"
	_, err := stack.ParseBytes("t.yaml", []byte(body))
	if err == nil {
		t.Fatal("expected error for bare name in extends")
	}
	if !stack.IsKind(err, stack.ErrInvalidExtends) {
		t.Errorf("error kind != invalid_extends: %v", err)
	}
}

func TestParseBytes_FlatRuleNameWithSlash_Rejected(t *testing.T) {
	body := "rules:\n  - bad/name\n"
	_, err := stack.ParseBytes("t.yaml", []byte(body))
	if err == nil {
		t.Fatal("expected error for nested rule name")
	}
	if !stack.IsKind(err, stack.ErrInvalidEntry) {
		t.Errorf("error kind != invalid_entry: %v", err)
	}
}

func TestParseBytes_UnknownField_Rejected(t *testing.T) {
	body := "made_up_key: 1\n"
	_, err := stack.ParseBytes("t.yaml", []byte(body))
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	if !stack.IsKind(err, stack.ErrUnknownField) {
		t.Errorf("error kind != unknown_field: %v", err)
	}
}

func TestParseInFS(t *testing.T) {
	fsys := fstest.MapFS{
		"stacks/team.yaml": &fstest.MapFile{Data: []byte("skills:\n  - foo\n")},
	}
	s, err := stack.ParseInFS(fsys, "stacks/team.yaml")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(s.Skills) != 1 || s.Skills[0].Name != "foo" {
		t.Errorf("skills = %+v", s.Skills)
	}
}

func TestEntriesFor(t *testing.T) {
	body := "skills:\n  - a\nrules:\n  - b\n"
	s, err := stack.ParseBytes("t.yaml", []byte(body))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(s.EntriesFor(definitions.CategorySkill)) != 1 {
		t.Errorf("EntriesFor(skill) = %+v", s.EntriesFor(definitions.CategorySkill))
	}
	if len(s.EntriesFor(definitions.CategoryAgent)) != 0 {
		t.Errorf("EntriesFor(agent) should be empty")
	}
}
