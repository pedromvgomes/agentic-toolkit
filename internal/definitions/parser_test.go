package definitions

import (
	"strings"
	"testing"
)

// ===== valid fixtures =====

func TestParse_ValidSkill(t *testing.T) {
	def, err := ParseFile("testdata/valid/skills/example/SKILL.md", CategorySkill)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := def.(*Skill)
	if !ok {
		t.Fatalf("got %T, want *Skill", def)
	}
	if s.Name != "example" {
		t.Errorf("name = %q, want %q", s.Name, "example")
	}
	if def.Category() != CategorySkill {
		t.Errorf("category = %q, want %q", def.Category(), CategorySkill)
	}
	if len(s.Platforms) != 2 {
		t.Errorf("platforms = %v, want 2 entries", s.Platforms)
	}
	if s.Extensions.Claude == nil {
		t.Fatalf("expected claude extensions populated")
	}
	if got := s.Extensions.Claude.AllowedTools; len(got) != 2 || got[0] != "Read" {
		t.Errorf("allowed_tools = %v, want [Read Grep]", got)
	}
	if !strings.Contains(s.Body, "Body of the skill.") {
		t.Errorf("body did not include expected text, got %q", s.Body)
	}
}

func TestParse_ValidRuleScoped(t *testing.T) {
	def, err := ParseFile("testdata/valid/rules/scoped.md", CategoryRule)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := def.(*Rule)
	if len(r.Paths) != 2 {
		t.Errorf("paths = %v, want 2", r.Paths)
	}
	if r.Always {
		t.Errorf("always = true, want false")
	}
}

func TestParse_ValidRuleAlways(t *testing.T) {
	def, err := ParseFile("testdata/valid/rules/always.md", CategoryRule)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := def.(*Rule)
	if !r.Always {
		t.Errorf("always = false, want true")
	}
	if r.Name != "always" {
		t.Errorf("derived name = %q, want %q", r.Name, "always")
	}
}

func TestParse_ValidInstruction(t *testing.T) {
	def, err := ParseFile("testdata/valid/instructions/notes.md", CategoryInstruction)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	i := def.(*Instruction)
	if i.Name != "notes" {
		t.Errorf("name = %q, want notes", i.Name)
	}
	if !strings.Contains(i.Body, "Always commit with a message.") {
		t.Errorf("body missing expected text: %q", i.Body)
	}
}

func TestParse_ValidAgent(t *testing.T) {
	def, err := ParseFile("testdata/valid/agents/reviewer.md", CategoryAgent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a := def.(*Agent)
	if a.Color != AgentColorBlue {
		t.Errorf("color = %q, want blue", a.Color)
	}
	if a.Extensions.Claude == nil || a.Extensions.OpenCode == nil {
		t.Fatalf("expected claude+opencode extensions populated")
	}
	if a.Extensions.Claude.PermissionMode != "plan" {
		t.Errorf("claude.permission_mode = %q, want plan", a.Extensions.Claude.PermissionMode)
	}
	if a.Extensions.OpenCode.Mode != "subagent" {
		t.Errorf("opencode.mode = %q, want subagent", a.Extensions.OpenCode.Mode)
	}
}

func TestParse_ValidCommandFlat(t *testing.T) {
	def, err := ParseFile("testdata/valid/commands/lint.md", CategoryCommand)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := def.(*Command)
	if c.Name != "lint" {
		t.Errorf("name = %q, want lint", c.Name)
	}
	if c.ArgumentHint != "[paths...]" {
		t.Errorf("argument_hint = %q", c.ArgumentHint)
	}
}

func TestParse_ValidHookCommand(t *testing.T) {
	def, err := ParseFile("testdata/valid/hooks/log-tools.yaml", CategoryHook)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h := def.(*Hook)
	if h.Event != "PreToolUse" {
		t.Errorf("event = %q", h.Event)
	}
	if h.Handler.Type != HandlerCommand {
		t.Errorf("handler.type = %q", h.Handler.Type)
	}
	if h.Timeout != 2000 {
		t.Errorf("timeout = %d", h.Timeout)
	}
}

func TestParse_ValidHookPrompt(t *testing.T) {
	def, err := ParseFile("testdata/valid/hooks/triage-prompt.yaml", CategoryHook)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h := def.(*Hook)
	if h.Handler.Type != HandlerPrompt {
		t.Errorf("handler.type = %q", h.Handler.Type)
	}
	if h.Handler.Model != "haiku" {
		t.Errorf("handler.model = %q", h.Handler.Model)
	}
	if h.Name != "triage-prompt" {
		t.Errorf("derived name = %q", h.Name)
	}
}

func TestParse_ValidMCPStdio(t *testing.T) {
	def, err := ParseFile("testdata/valid/mcp/filesystem.yaml", CategoryMCP)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := def.(*MCPServer)
	if m.Transport != TransportStdio {
		t.Errorf("transport = %q", m.Transport)
	}
	if m.Command == "" {
		t.Errorf("command is empty")
	}
	if m.URL != "" {
		t.Errorf("url should be empty for stdio, got %q", m.URL)
	}
}

func TestParse_ValidMCPHTTP(t *testing.T) {
	def, err := ParseFile("testdata/valid/mcp/remote-api.yaml", CategoryMCP)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := def.(*MCPServer)
	if m.Transport != TransportHTTP {
		t.Errorf("transport = %q", m.Transport)
	}
	if m.OAuth == nil || len(m.OAuth.Scopes) != 2 {
		t.Errorf("oauth.scopes not parsed: %+v", m.OAuth)
	}
}

// ===== invalid fixtures =====

func TestParse_InvalidCases(t *testing.T) {
	cases := []struct {
		name string
		path string
		cat  Category
		want ErrorKind
	}{
		{"no-frontmatter", "testdata/invalid/no-frontmatter.md", CategorySkill, ErrFrontmatterMissing},
		{"unclosed-frontmatter", "testdata/invalid/unclosed-frontmatter.md", CategorySkill, ErrFrontmatterUnclosed},
		{"unknown-field", "testdata/invalid/unknown-field.md", CategorySkill, ErrUnknownField},
		{"missing-description", "testdata/invalid/missing-description.md", CategorySkill, ErrMissingRequired},
		{"name-mismatch", "testdata/invalid/name-mismatch.md", CategoryRule, ErrInvalidName},
		{"unknown-platform", "testdata/invalid/unknown-platform.md", CategoryRule, ErrUnknownPlatform},
		{"extension-without-platform", "testdata/invalid/extension-without-platform.md", CategoryAgent, ErrPlatformExtension},
		{"unknown-color", "testdata/invalid/unknown-color.md", CategoryAgent, ErrUnknownColor},
		{"transport-conflict", "testdata/invalid/transport-conflict.yaml", CategoryMCP, ErrTransportConflict},
		{"unknown-transport", "testdata/invalid/unknown-transport.yaml", CategoryMCP, ErrUnknownTransport},
		{"handler-shape", "testdata/invalid/handler-shape.yaml", CategoryHook, ErrHandlerShape},
		{"unknown-handler", "testdata/invalid/unknown-handler.yaml", CategoryHook, ErrUnknownHandler},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseFile(tc.path, tc.cat)
			if err == nil {
				t.Fatalf("expected error of kind %s, got nil", tc.want)
			}
			if !IsKind(err, tc.want) {
				t.Errorf("got error kind != %s\n  err: %v", tc.want, err)
			}
		})
	}
}

// ===== name derivation =====

func TestDeriveName_Skill(t *testing.T) {
	got, err := deriveName(CategorySkill, "/x/skills/foo/SKILL.md", "foo/SKILL.md")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "foo" {
		t.Errorf("got %q, want foo", got)
	}
}

func TestDeriveName_Skill_BadShape(t *testing.T) {
	_, err := deriveName(CategorySkill, "/x/skills/foo.md", "foo.md")
	if err == nil {
		t.Fatalf("expected error for non-SKILL.md skill file")
	}
}

func TestDeriveName_NestedCommand(t *testing.T) {
	got, err := deriveName(CategoryCommand, "/x/commands/git/commit.md", "git/commit.md")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "git/commit" {
		t.Errorf("got %q, want git/commit", got)
	}
}

func TestDeriveName_NestedRuleRejected(t *testing.T) {
	_, err := deriveName(CategoryRule, "/x/rules/sub/rule.md", "sub/rule.md")
	if err == nil {
		t.Fatalf("expected error for nested rule path")
	}
	if !IsKind(err, ErrInvalidName) {
		t.Errorf("got %v, want ErrInvalidName", err)
	}
}
