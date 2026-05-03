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
	// Wording check — "flat" is jargon; the message should say what the
	// concrete restriction is.
	if !strings.Contains(err.Error(), "cannot contain '/'") {
		t.Errorf("error should explain '/' restriction in plain words: %v", err)
	}
}

func TestParseBytes_URLShapedBareName_HintsAtGitBoundary(t *testing.T) {
	// gitlab.com is intentionally NOT in the allowlist (nested groups make
	// owner/repo ambiguous). The error should still be helpful: detect the
	// host shape and tell the user to add `.git/`.
	body := "skills:\n  - gitlab.com/group/repo/path/to/skill\n"
	_, err := stack.ParseBytes("t.yaml", []byte(body))
	if err == nil {
		t.Fatal("expected error for URL-shaped bare name")
	}
	msg := err.Error()
	if !strings.Contains(msg, ".git/") {
		t.Errorf("error should mention .git/ boundary: %v", err)
	}
	if !strings.Contains(msg, "looks like a URL") {
		t.Errorf("error should call out the URL shape: %v", err)
	}
}

func TestParseBytes_ProviderAutoSplit(t *testing.T) {
	// github.com / bitbucket.org / codeberg.org URLs without an explicit
	// `.git/` boundary are auto-normalised: agtk infers it after
	// owner/repo so users can paste the form they see in a browser.
	cases := []struct {
		name    string
		entry   string
		wantURL string
		wantRef string
	}{
		{
			name:    "github with subpath",
			entry:   "github.com/pedromvgomes/gt/agentic/skills/use-gt",
			wantURL: "github.com/pedromvgomes/gt.git/agentic/skills/use-gt",
		},
		{
			name:    "github with @ref",
			entry:   "github.com/foo/bar/skills/x@v1.2",
			wantURL: "github.com/foo/bar.git/skills/x",
			wantRef: "v1.2",
		},
		{
			name:    "github bare repo (no subpath)",
			entry:   "github.com/foo/bar",
			wantURL: "github.com/foo/bar.git",
		},
		{
			name:    "bitbucket with subpath",
			entry:   "bitbucket.org/team/proj/rules/style.md",
			wantURL: "bitbucket.org/team/proj.git/rules/style.md",
		},
		{
			name:    "codeberg with subpath",
			entry:   "codeberg.org/owner/repo/agents/foo",
			wantURL: "codeberg.org/owner/repo.git/agents/foo",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := "skills:\n  - " + tc.entry + "\n"
			s, err := stack.ParseBytes("t.yaml", []byte(body))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(s.Skills) != 1 {
				t.Fatalf("skills = %+v", s.Skills)
			}
			got := s.Skills[0]
			if got.Kind != stack.RefURL {
				t.Errorf("kind = %v, want RefURL", got.Kind)
			}
			if got.URL != tc.wantURL {
				t.Errorf("URL = %q, want %q", got.URL, tc.wantURL)
			}
			if got.Ref != tc.wantRef {
				t.Errorf("Ref = %q, want %q", got.Ref, tc.wantRef)
			}
		})
	}
}

func TestParseBytes_ProviderAutoSplit_Extends(t *testing.T) {
	body := "extends:\n  - github.com/foo/bar/stacks/default.yaml@main\n"
	s, err := stack.ParseBytes("t.yaml", []byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.Extends) != 1 {
		t.Fatalf("extends = %+v", s.Extends)
	}
	got := s.Extends[0]
	if got.Kind != stack.RefURL {
		t.Errorf("kind = %v, want RefURL", got.Kind)
	}
	if got.URL != "github.com/foo/bar.git/stacks/default.yaml" {
		t.Errorf("URL = %q", got.URL)
	}
	if got.Ref != "main" {
		t.Errorf("Ref = %q", got.Ref)
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
