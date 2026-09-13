package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/claude"
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
)

// localBody renders a `local:` block from category key → directory. The
// "context" key names a file rather than a directory.
func localBody(dirs map[string]string) string {
	out := "local:\n"
	for _, key := range []string{"context", "skills", "agents", "rules", "instructions", "commands", "hooks", "mcp", "settings"} {
		if dir, ok := dirs[key]; ok {
			out += "  " + key + ": " + dir + "\n"
		}
	}
	return out
}

func validCommandBody(description string) string {
	return "---\ndescription: " + description + "\n---\n\nbody\n"
}

func validHookBody(name string) string {
	return "name: " + name + "\ndescription: A hook.\nevent: PreToolUse\nhandler:\n  type: command\n  command: \"true\"\n"
}

func validMCPBody(name string) string {
	return "name: " + name + "\ndescription: An MCP server.\ntransport: stdio\ncommand: mcp-server\n"
}

// validSettingBody sets one top-level key, so two settings definitions can be
// made to contend for it.
func validSettingBody(name, model string) string {
	return "name: " + name + "\ndescription: A setting.\nvalue:\n  model: " + model + "\n"
}

// resolveLocal parses the entry manifest in entryFS and resolves it.
func resolveLocal(t *testing.T, entryFS fstest.MapFS) (*resolver.Plan, error) {
	t.Helper()
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}
	return resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", newFakeProvider())
}

// ===== per-category scan shapes =====

func TestResolve_LocalScan_EachCategoryShape(t *testing.T) {
	cases := []struct {
		key      string
		dir      string
		files    map[string]string
		category definitions.Category
		wantName string
	}{
		{
			key: "skills", dir: "./mine/skills",
			files:    map[string]string{"mine/skills/tidy/SKILL.md": validSkillBody("Tidy skill")},
			category: definitions.CategorySkill, wantName: "tidy",
		},
		{
			key: "agents", dir: "./mine/agents",
			files:    map[string]string{"mine/agents/scout/AGENT.md": validAgentBody("Scout agent")},
			category: definitions.CategoryAgent, wantName: "scout",
		},
		{
			key: "rules", dir: "./mine/rules",
			files:    map[string]string{"mine/rules/style.md": validRuleBody("Style rule")},
			category: definitions.CategoryRule, wantName: "style",
		},
		{
			key: "instructions", dir: "./mine/instructions",
			files:    map[string]string{"mine/instructions/git.md": validInstructionBody("Git instruction")},
			category: definitions.CategoryInstruction, wantName: "git",
		},
		{
			key: "commands", dir: "./mine/commands",
			files:    map[string]string{"mine/commands/ship.md": validCommandBody("Ship command")},
			category: definitions.CategoryCommand, wantName: "ship",
		},
		{
			key: "hooks", dir: "./mine/hooks",
			files:    map[string]string{"mine/hooks/guard.yaml": validHookBody("guard")},
			category: definitions.CategoryHook, wantName: "guard",
		},
		{
			key: "mcp", dir: "./mine/mcp",
			files:    map[string]string{"mine/mcp/files.yaml": validMCPBody("files")},
			category: definitions.CategoryMCP, wantName: "files",
		},
		{
			key: "settings", dir: "./mine/settings",
			files:    map[string]string{"mine/settings/model.yaml": validSettingBody("model", "opus")},
			category: definitions.CategorySetting, wantName: "model",
		},
	}

	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			files := map[string]string{
				".agentic-toolkit.yaml": localBody(map[string]string{tc.key: tc.dir}),
			}
			for p, body := range tc.files {
				files[p] = body
			}

			plan, err := resolveLocal(t, makeMapFS(files))
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if len(plan.Definitions) != 1 {
				t.Fatalf("definitions = %+v, want 1", plan.Definitions)
			}
			d := plan.Definitions[0]
			if d.Category != tc.category || d.Name != tc.wantName {
				t.Errorf("got (%s, %q), want (%s, %q)", d.Category, d.Name, tc.category, tc.wantName)
			}
			wantStack := "local." + tc.category.CategoryDir()
			if d.StackName != wantStack {
				t.Errorf("stack name = %q, want %q", d.StackName, wantStack)
			}
			last := plan.StackOrder[len(plan.StackOrder)-1]
			if last != wantStack {
				t.Errorf("StackOrder = %v, want %q last", plan.StackOrder, wantStack)
			}
		})
	}
}

func TestResolve_LocalScan_NestedCommandIsNamespaced(t *testing.T) {
	plan, err := resolveLocal(t, makeMapFS(map[string]string{
		".agentic-toolkit.yaml":          localBody(map[string]string{"commands": "./mine/commands"}),
		"mine/commands/foo/bar.md":       validCommandBody("Nested command"),
		"mine/commands/flat.md":          validCommandBody("Flat command"),
		"mine/commands/foo/notes.txt":    "not a definition\n",
		"mine/commands/deep/a/b/here.md": validCommandBody("Deep command"),
	}))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	got := map[string]bool{}
	for _, d := range plan.Definitions {
		got[d.Name] = true
	}
	for _, want := range []string{"foo/bar", "flat", "deep/a/b/here"} {
		if !got[want] {
			t.Errorf("command %q missing; got %v", want, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("definitions = %v, want exactly the three markdown files", got)
	}
}

func TestResolve_LocalScan_InstructionsCarryFilenameOrder(t *testing.T) {
	plan, err := resolveLocal(t, makeMapFS(map[string]string{
		".agentic-toolkit.yaml":         localBody(map[string]string{"instructions": "./mine/instructions"}),
		"mine/instructions/10-git.md":   validInstructionBody("Git"),
		"mine/instructions/20-style.md": validInstructionBody("Style"),
		"mine/instructions/30-test.md":  validInstructionBody("Test"),
	}))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := map[string]int{"10-git": 1, "20-style": 2, "30-test": 3}
	for _, d := range plan.Definitions {
		if d.ScanOrder != want[d.Name] {
			t.Errorf("%s scan order = %d, want %d", d.Name, d.ScanOrder, want[d.Name])
		}
	}
}

func TestResolve_LocalScan_NamedEntryCarriesNoScanOrder(t *testing.T) {
	plan, err := resolveLocal(t, makeMapFS(map[string]string{
		".agentic-toolkit.yaml": stackBody(nil, map[string][]string{
			"instructions": {"git"},
		}),
		"definitions/instructions/git.md": validInstructionBody("Git"),
	}))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if plan.Definitions[0].ScanOrder != 0 {
		t.Errorf("scan order = %d, want 0 for a definition the stack named", plan.Definitions[0].ScanOrder)
	}
}

// ===== refusals =====

func TestResolve_LocalScan_MissingDirectoryRefused(t *testing.T) {
	_, err := resolveLocal(t, makeMapFS(map[string]string{
		".agentic-toolkit.yaml": localBody(map[string]string{"skills": "./mine/skills"}),
	}))
	if err == nil {
		t.Fatal("resolve: want error for a local directory that does not exist")
	}
	if !strings.Contains(err.Error(), "local.skills") {
		t.Errorf("error = %v, want it to name local.skills", err)
	}
}

func TestResolve_LocalScan_ReportsEveryBadDirectory(t *testing.T) {
	_, err := resolveLocal(t, makeMapFS(map[string]string{
		".agentic-toolkit.yaml": localBody(map[string]string{
			"skills":   "./mine/skills",
			"rules":    "./mine/rules",
			"settings": "./mine/settings",
		}),
		"mine/rules/style.md": validRuleBody("Style rule"),
	}))
	if err == nil {
		t.Fatal("resolve: want error")
	}
	for _, want := range []string{"local.skills", "local.settings"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to name %s", err, want)
		}
	}
}

func TestResolve_LocalScan_DuplicateNameRefused(t *testing.T) {
	_, err := resolveLocal(t, makeMapFS(map[string]string{
		".agentic-toolkit.yaml": localBody(map[string]string{"rules": "./mine/rules"}),
		"mine/rules/a.md":       "---\nname: style\ndescription: One\nalways: true\n---\n\nbody\n",
		"mine/rules/b.md":       "---\nname: style\ndescription: Another\nalways: true\n---\n\nbody\n",
	}))
	if err == nil {
		t.Fatal("resolve: want error for two local rules declaring the same name")
	}
	if !strings.Contains(err.Error(), "style") {
		t.Errorf("error = %v, want it to name the duplicated definition", err)
	}
}

// ===== local wins =====

// extendsWithSkill serves a stack that lists one skill, both under the same
// fake source, so a local scan has something from extends: to beat.
func extendsProvider(files map[string]string) *fakeProvider {
	return newFakeProvider().register("github.com/upstream/base.git", "main", makeMapFS(files))
}

func TestResolve_LocalScan_OverridesExtendsDefinition(t *testing.T) {
	provider := extendsProvider(map[string]string{
		"stacks/base.yaml": stackBody(nil, map[string][]string{
			"skills": {"tidy"},
		}),
		"definitions/skills/tidy/SKILL.md": validSkillBody("Upstream tidy"),
	})
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": stackBody([]string{"github.com/upstream/base.git/stacks/base.yaml@main"}, nil) +
			localBody(map[string]string{"skills": "./mine/skills"}),
		"mine/skills/tidy/SKILL.md": validSkillBody("Local tidy"),
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
	if got := plan.Definitions[0].StackName; got != "local.skills" {
		t.Errorf("winner stack = %q, want local.skills", got)
	}
	var overrides []string
	for _, d := range plan.Diagnostics {
		if d.Kind == resolver.DiagOverride {
			overrides = append(overrides, d.Message)
		}
	}
	if len(overrides) != 1 || !strings.Contains(overrides[0], `"local.skills"`) {
		t.Errorf("override diagnostics = %v, want one naming local.skills", overrides)
	}
}

func TestResolve_LocalScan_NestedCommandOverridesExtendsDefinition(t *testing.T) {
	provider := extendsProvider(map[string]string{
		"stacks/base.yaml": stackBody(nil, map[string][]string{
			"commands": {"foo/bar"},
		}),
		// A namespaced command carries its full name in frontmatter: the
		// filename stem alone is just the last segment.
		"definitions/commands/foo/bar.md": "---\nname: foo/bar\ndescription: Upstream nested command\n---\n\nbody\n",
	})
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": stackBody([]string{"github.com/upstream/base.git/stacks/base.yaml@main"}, nil) +
			localBody(map[string]string{"commands": "./mine/commands"}),
		"mine/commands/foo/bar.md": validCommandBody("Local nested command"),
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
	d := plan.Definitions[0]
	if d.Name != "foo/bar" || d.StackName != "local.commands" {
		t.Errorf("winner = (%q, %q), want (foo/bar, local.commands)", d.Name, d.StackName)
	}
}

// A locally scanned setting has to win the settings merge, not merely the
// override diagnostic. Adapters order contributions by the index of StackName
// in Plan.StackOrder, and an identifier that is not in that list reads as -1 —
// sorting first, so the local contribution would be overwritten by every stack
// reached through extends:.
func TestResolve_LocalScan_SettingWinsRenderedMerge(t *testing.T) {
	provider := extendsProvider(map[string]string{
		"stacks/base.yaml": stackBody(nil, map[string][]string{
			"settings": {"zz-upstream-model"},
		}),
		"definitions/settings/zz-upstream-model.yaml": validSettingBody("zz-upstream-model", "from-extends"),
	})
	entryFS := makeMapFS(map[string]string{
		".agentic-toolkit.yaml": stackBody([]string{"github.com/upstream/base.git/stacks/base.yaml@main"}, nil) +
			localBody(map[string]string{"settings": "./mine/settings"}),
		"mine/settings/aa-local-model.yaml": validSettingBody("aa-local-model", "from-local"),
	})
	st, err := stack.ParseInFS(entryFS, ".agentic-toolkit.yaml")
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}
	plan, err := resolver.Resolve(st, entryFS, ".agentic-toolkit.yaml", provider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")
	if err := claude.Render(plan, claude.Options{
		Scope: claude.ScopeProject, ScopeRoot: scopeRoot, ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("render: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(scopeRoot, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("settings.json: %v\n%s", err, raw)
	}
	if settings.Model != "from-local" {
		t.Errorf("model = %q, want from-local (StackOrder = %v)", settings.Model, plan.StackOrder)
	}
}
