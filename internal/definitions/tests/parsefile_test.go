package tests

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
)

// ===== name resolution =====

func TestParseFile_NameFromFrontmatter(t *testing.T) {
	fsys := fstest.MapFS{
		"any-name.md": &fstest.MapFile{Data: []byte("---\n" +
			"name: explicit-name\n" +
			"description: A rule.\n" +
			"---\n\nbody\n")},
	}
	def, err := definitions.ParseFile(fsys, definitions.CategoryRule, "any-name.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := def.GetCommon().Name; got != "explicit-name" {
		t.Errorf("name = %q, want %q (frontmatter wins)", got, "explicit-name")
	}
}

func TestParseFile_NameFromFilenameStem(t *testing.T) {
	fsys := fstest.MapFS{
		"my-rule.md": &fstest.MapFile{Data: []byte("---\n" +
			"description: A rule.\n" +
			"---\n\nbody\n")},
	}
	def, err := definitions.ParseFile(fsys, definitions.CategoryRule, "my-rule.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := def.GetCommon().Name; got != "my-rule" {
		t.Errorf("name = %q, want stem fallback %q", got, "my-rule")
	}
}

// ===== nested commands =====

func TestParseFile_CommandWithNestedName(t *testing.T) {
	fsys := fstest.MapFS{
		"commit.md": &fstest.MapFile{Data: []byte("---\n" +
			"name: git/commit\n" +
			"description: Commit changes.\n" +
			"---\n\nbody\n")},
	}
	def, err := definitions.ParseFile(fsys, definitions.CategoryCommand, "commit.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := def.GetCommon().Name; got != "git/commit" {
		t.Errorf("name = %q, want %q", got, "git/commit")
	}
}

func TestParseFile_CommandFlatStem(t *testing.T) {
	fsys := fstest.MapFS{
		"deploy.md": &fstest.MapFile{Data: []byte("---\n" +
			"description: Deploy.\n" +
			"---\n\nbody\n")},
	}
	def, err := definitions.ParseFile(fsys, definitions.CategoryCommand, "deploy.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := def.GetCommon().Name; got != "deploy" {
		t.Errorf("name = %q, want %q", got, "deploy")
	}
}

// ===== nesting validation: non-command file cats reject '/' in name =====

func TestParseFile_RuleRejectsNestedFrontmatterName(t *testing.T) {
	fsys := fstest.MapFS{
		"x.md": &fstest.MapFile{Data: []byte("---\n" +
			"name: group/style\n" +
			"description: A rule.\n" +
			"---\n\nbody\n")},
	}
	_, err := definitions.ParseFile(fsys, definitions.CategoryRule, "x.md")
	if err == nil {
		t.Fatalf("expected error for nested rule name")
	}
	if !strings.Contains(err.Error(), "must be flat") {
		t.Errorf("error = %v, want flat-name error", err)
	}
}

func TestParseFile_CommandRejectsBadShape(t *testing.T) {
	cases := []struct{ name, frontmatter string }{
		{"leading-slash", "/foo"},
		{"trailing-slash", "foo/"},
		{"dotdot", "foo/../bar"},
		{"empty-segment", "foo//bar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := "---\nname: " + tc.frontmatter + "\ndescription: c.\n---\n\n"
			fsys := fstest.MapFS{"f.md": &fstest.MapFile{Data: []byte(body)}}
			if _, err := definitions.ParseFile(fsys, definitions.CategoryCommand, "f.md"); err == nil {
				t.Fatalf("expected error for command name %q", tc.frontmatter)
			}
		})
	}
}

// ===== bundle categories rejected =====

func TestParseFile_RejectsBundleCategories(t *testing.T) {
	fsys := fstest.MapFS{
		"x.md": &fstest.MapFile{Data: []byte("---\ndescription: x.\n---\n\n")},
	}
	for _, cat := range []definitions.Category{definitions.CategorySkill, definitions.CategoryAgent} {
		t.Run(string(cat), func(t *testing.T) {
			_, err := definitions.ParseFile(fsys, cat, "x.md")
			if err == nil {
				t.Fatalf("expected error for bundle category %q", cat)
			}
			if !strings.Contains(err.Error(), "ParseFile") {
				t.Errorf("error = %v, want ParseFile-shaped error", err)
			}
		})
	}
}

// ===== YAML categories: hook & mcp =====

func TestParseFile_HookYAML(t *testing.T) {
	body := "name: log-tools\n" +
		"description: Log tool calls.\n" +
		"event: PreToolUse\n" +
		"handler:\n" +
		"  type: command\n" +
		"  command: echo hi\n"
	fsys := fstest.MapFS{"hook.yaml": &fstest.MapFile{Data: []byte(body)}}
	def, err := definitions.ParseFile(fsys, definitions.CategoryHook, "hook.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h := def.(*definitions.Hook)
	if h.Name != "log-tools" {
		t.Errorf("name = %q, want %q", h.Name, "log-tools")
	}
	if h.Event != "PreToolUse" {
		t.Errorf("event = %q", h.Event)
	}
}

func TestParseFile_HookStemFallback(t *testing.T) {
	body := "description: A hook.\n" +
		"event: PreToolUse\n" +
		"handler:\n" +
		"  type: command\n" +
		"  command: echo hi\n"
	fsys := fstest.MapFS{"triage.yaml": &fstest.MapFile{Data: []byte(body)}}
	def, err := definitions.ParseFile(fsys, definitions.CategoryHook, "triage.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := def.GetCommon().Name; got != "triage" {
		t.Errorf("name = %q, want stem fallback %q", got, "triage")
	}
}

func TestParseFile_MCPYAML(t *testing.T) {
	body := "name: filesystem\n" +
		"description: Filesystem MCP.\n" +
		"transport: stdio\n" +
		"command: /usr/bin/mcp-fs\n"
	fsys := fstest.MapFS{"fs.yaml": &fstest.MapFile{Data: []byte(body)}}
	def, err := definitions.ParseFile(fsys, definitions.CategoryMCP, "fs.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := def.(*definitions.MCPServer)
	if m.Name != "filesystem" {
		t.Errorf("name = %q, want %q", m.Name, "filesystem")
	}
	if m.Transport != definitions.TransportStdio {
		t.Errorf("transport = %q", m.Transport)
	}
}

// ===== file-not-found =====

func TestParseFile_MissingFile(t *testing.T) {
	fsys := fstest.MapFS{}
	_, err := definitions.ParseFile(fsys, definitions.CategoryRule, "missing.md")
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
}

// ===== layout-agnostic: filename with no frontmatter name and arbitrary parent =====

func TestParseFile_LayoutAgnostic(t *testing.T) {
	// fsys is rooted at the parent dir; the file lives at the root of the FS,
	// regardless of where in the remote repo it actually sat. ParseFile does
	// not care about anything outside the file itself.
	fsys := fstest.MapFS{
		"plan-approval.md": &fstest.MapFile{Data: []byte("---\n" +
			"description: Surface a plan before doing anything irreversible.\n" +
			"---\n\nbody\n")},
	}
	def, err := definitions.ParseFile(fsys, definitions.CategoryInstruction, "plan-approval.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := def.GetCommon().Name; got != "plan-approval" {
		t.Errorf("name = %q, want %q", got, "plan-approval")
	}
}
