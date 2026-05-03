package tests

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
)

// ===== valid fixtures =====

func TestParse_ValidSkill(t *testing.T) {
	def, err := definitions.ParseInCatalog(validFS(), "definitions/skills/example/SKILL.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := def.(*definitions.Skill)
	if !ok {
		t.Fatalf("got %T, want *Skill", def)
	}
	if s.Name != "example" {
		t.Errorf("name = %q, want %q", s.Name, "example")
	}
	if def.Category() != definitions.CategorySkill {
		t.Errorf("category = %q, want %q", def.Category(), definitions.CategorySkill)
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
	def, err := definitions.ParseInCatalog(validFS(), "definitions/rules/scoped.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := def.(*definitions.Rule)
	if len(r.Paths) != 2 {
		t.Errorf("paths = %v, want 2", r.Paths)
	}
	if r.Always {
		t.Errorf("always = true, want false")
	}
}

func TestParse_ValidRuleAlways(t *testing.T) {
	def, err := definitions.ParseInCatalog(validFS(), "definitions/rules/always.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := def.(*definitions.Rule)
	if !r.Always {
		t.Errorf("always = false, want true")
	}
	if r.Name != "always" {
		t.Errorf("derived name = %q, want %q", r.Name, "always")
	}
}

// Rules accept a bare-markdown form: no `---` frontmatter at all. The
// whole file is the body, the name comes from the filename, and the
// description is empty. Other markdown categories (skills, agents,
// instructions, commands) still require frontmatter.
func TestParse_RuleBareMarkdown(t *testing.T) {
	def, err := definitions.ParseInCatalog(validFS(), "definitions/rules/bare.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := def.(*definitions.Rule)
	if r.Name != "bare" {
		t.Errorf("derived name = %q, want %q", r.Name, "bare")
	}
	if r.Description != "" {
		t.Errorf("description = %q, want empty for bare-markdown rule", r.Description)
	}
	if !strings.Contains(r.Body, "Just a plain markdown rule") {
		t.Errorf("body should contain entire file contents, got %q", r.Body)
	}
	// Body must NOT have a synthesized frontmatter block prepended.
	if strings.HasPrefix(r.Body, "---") {
		t.Errorf("bare-markdown body should not start with `---`, got %q", r.Body[:20])
	}
}

// Rules also accept frontmatter that omits the description. Other
// metadata (paths/always) still parses.
func TestParse_RuleFrontmatterWithoutDescription(t *testing.T) {
	def, err := definitions.ParseInCatalog(validFS(), "definitions/rules/empty-description.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := def.(*definitions.Rule)
	if r.Description != "" {
		t.Errorf("description = %q, want empty", r.Description)
	}
	if len(r.Paths) != 1 || r.Paths[0] != "src/**" {
		t.Errorf("paths = %v, want [src/**]", r.Paths)
	}
}

func TestParse_ValidInstruction(t *testing.T) {
	def, err := definitions.ParseInCatalog(validFS(), "definitions/instructions/notes.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	i := def.(*definitions.Instruction)
	if i.Name != "notes" {
		t.Errorf("name = %q, want notes", i.Name)
	}
	if !strings.Contains(i.Body, "Always commit with a message.") {
		t.Errorf("body missing expected text: %q", i.Body)
	}
}

func TestParse_ValidAgent(t *testing.T) {
	def, err := definitions.ParseInCatalog(validFS(), "definitions/agents/reviewer/AGENT.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a := def.(*definitions.Agent)
	if a.Color != definitions.AgentColorBlue {
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
	def, err := definitions.ParseInCatalog(validFS(), "definitions/commands/lint.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := def.(*definitions.Command)
	if c.Name != "lint" {
		t.Errorf("name = %q, want lint", c.Name)
	}
	if c.ArgumentHint != "[paths...]" {
		t.Errorf("argument_hint = %q", c.ArgumentHint)
	}
}

func TestParse_ValidCommandNested(t *testing.T) {
	def, err := definitions.ParseInCatalog(validFS(), "definitions/commands/git/commit.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := def.(*definitions.Command)
	if c.Name != "git/commit" {
		t.Errorf("name = %q, want git/commit", c.Name)
	}
}

func TestParse_ValidHookCommand(t *testing.T) {
	def, err := definitions.ParseInCatalog(validFS(), "definitions/hooks/log-tools.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h := def.(*definitions.Hook)
	if h.Event != "PreToolUse" {
		t.Errorf("event = %q", h.Event)
	}
	if h.Handler.Type != definitions.HandlerCommand {
		t.Errorf("handler.type = %q", h.Handler.Type)
	}
	if h.Timeout != 2000 {
		t.Errorf("timeout = %d", h.Timeout)
	}
}

func TestParse_ValidHookPrompt(t *testing.T) {
	def, err := definitions.ParseInCatalog(validFS(), "definitions/hooks/triage-prompt.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h := def.(*definitions.Hook)
	if h.Handler.Type != definitions.HandlerPrompt {
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
	def, err := definitions.ParseInCatalog(validFS(), "definitions/mcp/filesystem.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := def.(*definitions.MCPServer)
	if m.Transport != definitions.TransportStdio {
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
	def, err := definitions.ParseInCatalog(validFS(), "definitions/mcp/remote-api.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := def.(*definitions.MCPServer)
	if m.Transport != definitions.TransportHTTP {
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
		want definitions.ErrorKind
	}{
		{"no-frontmatter", "definitions/skills/no-frontmatter/SKILL.md", definitions.ErrFrontmatterMissing},
		{"unclosed-frontmatter", "definitions/skills/unclosed-frontmatter/SKILL.md", definitions.ErrFrontmatterUnclosed},
		{"unknown-field", "definitions/skills/unknown-field/SKILL.md", definitions.ErrUnknownField},
		{"missing-description", "definitions/skills/missing-description/SKILL.md", definitions.ErrMissingRequired},
		{"name-mismatch", "definitions/rules/name-mismatch.md", definitions.ErrInvalidName},
		{"unknown-platform", "definitions/rules/unknown-platform.md", definitions.ErrUnknownPlatform},
		{"extension-without-platform", "definitions/agents/extension-without-platform/AGENT.md", definitions.ErrPlatformExtension},
		{"unknown-color", "definitions/agents/unknown-color/AGENT.md", definitions.ErrUnknownColor},
		{"transport-conflict", "definitions/mcp/transport-conflict.yaml", definitions.ErrTransportConflict},
		{"unknown-transport", "definitions/mcp/unknown-transport.yaml", definitions.ErrUnknownTransport},
		{"handler-shape", "definitions/hooks/handler-shape.yaml", definitions.ErrHandlerShape},
		{"unknown-handler", "definitions/hooks/unknown-handler.yaml", definitions.ErrUnknownHandler},
		{"skill-bad-shape", "definitions/skills/badshape.md", definitions.ErrInvalidName},
		{"rule-nested", "definitions/rules/sub/nested.md", definitions.ErrInvalidName},
	}
	fsys := invalidFS()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := definitions.ParseInCatalog(fsys, tc.path)
			if err == nil {
				t.Fatalf("expected error of kind %s, got nil", tc.want)
			}
			if !definitions.IsKind(err, tc.want) {
				t.Errorf("got error kind != %s\n  err: %v", tc.want, err)
			}
		})
	}
}

// ===== ParseInCatalog argument validation =====

func TestParseInCatalog_RejectsNonCatalogPaths(t *testing.T) {
	fsys := fstest.MapFS{
		"random/file.md": &fstest.MapFile{Data: []byte("---\n---\n")},
	}
	if _, err := definitions.ParseInCatalog(fsys, "random/file.md"); err == nil {
		t.Fatalf("expected error for non-catalog path")
	} else if !definitions.IsKind(err, definitions.ErrUnknownCategory) {
		t.Errorf("got %v, want ErrUnknownCategory", err)
	}
}

func TestParseInCatalog_RejectsUnknownCategory(t *testing.T) {
	fsys := fstest.MapFS{
		"definitions/widgets/foo.md": &fstest.MapFile{Data: []byte("---\n---\n")},
	}
	if _, err := definitions.ParseInCatalog(fsys, "definitions/widgets/foo.md"); err == nil {
		t.Fatalf("expected error for unknown category dir")
	} else if !definitions.IsKind(err, definitions.ErrUnknownCategory) {
		t.Errorf("got %v, want ErrUnknownCategory", err)
	}
}
