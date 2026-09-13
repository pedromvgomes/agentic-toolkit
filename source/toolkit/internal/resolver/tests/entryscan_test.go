package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/claude"
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// ===== per-category scan shapes =====

func TestResolve_Scan_EachCategoryShape(t *testing.T) {
	cases := []struct {
		name     string
		files    map[string]string
		category definitions.Category
		wantName string
	}{
		{
			name:     "skills",
			files:    map[string]string{"agentic/skills/tidy/SKILL.md": validSkillBody("Tidy skill")},
			category: definitions.CategorySkill, wantName: "tidy",
		},
		{
			name:     "agents",
			files:    map[string]string{"agentic/agents/scout/AGENT.md": validAgentBody("Scout agent")},
			category: definitions.CategoryAgent, wantName: "scout",
		},
		{
			name:     "rules",
			files:    map[string]string{"agentic/rules/style.md": validRuleBody("Style rule")},
			category: definitions.CategoryRule, wantName: "style",
		},
		{
			name:     "instructions",
			files:    map[string]string{"agentic/instructions/git.md": validInstructionBody("Git instruction")},
			category: definitions.CategoryInstruction, wantName: "git",
		},
		{
			name:     "commands",
			files:    map[string]string{"agentic/commands/ship.md": validCommandBody("Ship command")},
			category: definitions.CategoryCommand, wantName: "ship",
		},
		{
			name:     "hooks",
			files:    map[string]string{"agentic/hooks/guard.yaml": validHookBody("guard")},
			category: definitions.CategoryHook, wantName: "guard",
		},
		{
			name:     "mcp",
			files:    map[string]string{"agentic/mcp/files.yaml": validMCPBody("files")},
			category: definitions.CategoryMCP, wantName: "files",
		},
		{
			name:     "settings",
			files:    map[string]string{"agentic/settings/model.yaml": validSettingBody("model", "opus")},
			category: definitions.CategorySetting, wantName: "model",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{entryManifestPath: entryBody(nil, "")}
			for p, body := range tc.files {
				files[p] = body
			}

			plan, err := resolveEntry(t, makeMapFS(files), newFakeProvider())
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
			if d.StackName != "" {
				t.Errorf("stack name = %q, want the entry manifest's own identifier", d.StackName)
			}
			if d.IsContext {
				t.Error("a scanned definition is not the context file")
			}
			if last := plan.StackOrder[len(plan.StackOrder)-1]; last != "" {
				t.Errorf("StackOrder = %v, want the entry manifest last", plan.StackOrder)
			}
		})
	}
}

// `root:` moves every scanned category at once: the categories are found
// under it by convention, not configured one by one.
func TestResolve_Scan_HonoursConfiguredRoot(t *testing.T) {
	plan, err := resolveEntry(t, makeMapFS(map[string]string{
		entryManifestPath:           entryBody(nil, "root: mine\n"),
		"mine/skills/tidy/SKILL.md": validSkillBody("Tidy skill"),
		// Under the default root, which a configured one replaces rather
		// than adds to.
		"agentic/skills/other/SKILL.md": validSkillBody("Other skill"),
	}), newFakeProvider())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Name != "tidy" {
		t.Errorf("definitions = %+v, want only what the configured root holds", plan.Definitions)
	}
}

func TestResolve_Scan_NestedCommandIsNamespaced(t *testing.T) {
	plan, err := resolveEntry(t, makeMapFS(map[string]string{
		entryManifestPath:                   entryBody(nil, ""),
		"agentic/commands/foo/bar.md":       validCommandBody("Nested command"),
		"agentic/commands/flat.md":          validCommandBody("Flat command"),
		"agentic/commands/foo/notes.txt":    "not a definition\n",
		"agentic/commands/deep/a/b/here.md": validCommandBody("Deep command"),
	}), newFakeProvider())
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

// A file the category's shape does not cover is not a failed definition: a
// repo keeps READMEs and notes beside what it ships.
func TestResolve_Scan_IgnoresFilesTheShapeDoesNotCover(t *testing.T) {
	plan, err := resolveEntry(t, makeMapFS(map[string]string{
		entryManifestPath:             entryBody(nil, ""),
		"agentic/rules/style.md":      validRuleBody("Style rule"),
		"agentic/rules/README.txt":    "notes about the rules\n",
		"agentic/settings/model.yaml": validSettingBody("model", "opus"),
		"agentic/settings/notes.md":   "notes about the settings\n",
	}), newFakeProvider())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 2 {
		t.Errorf("definitions = %+v, want the rule and the setting only", plan.Definitions)
	}
}

// ===== missing and malformed content =====

// A repo contributes the categories it has. Requiring eight directories to
// use one of them would make an empty tree a precondition for the manifest.
func TestResolve_Scan_MissingCategoryDirectoryIsEmpty(t *testing.T) {
	plan, err := resolveEntry(t, makeMapFS(map[string]string{
		entryManifestPath:        entryBody(nil, ""),
		"agentic/rules/style.md": validRuleBody("Style rule"),
	}), newFakeProvider())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %+v, want the one category that exists", plan.Definitions)
	}
	if len(plan.Diagnostics) != 0 {
		t.Errorf("diagnostics = %+v, want none for the seven absent categories", plan.Diagnostics)
	}
}

// A root that does not exist at all is the same state: a consumer composing
// stacks and shipping nothing of its own is a whole repo's worth of absent
// directories.
func TestResolve_Scan_MissingRootIsEmpty(t *testing.T) {
	plan, err := resolveEntry(t, makeMapFS(map[string]string{
		entryManifestPath: entryBody(nil, ""),
	}), newFakeProvider())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 0 || len(plan.Diagnostics) != 0 {
		t.Errorf("plan = %+v / %+v, want nothing found and nothing said", plan.Definitions, plan.Diagnostics)
	}
}

func TestResolve_Scan_DuplicateNameRefused(t *testing.T) {
	_, err := resolveEntry(t, makeMapFS(map[string]string{
		entryManifestPath:    entryBody(nil, ""),
		"agentic/rules/a.md": "---\nname: style\ndescription: One\nalways: true\n---\n\nbody\n",
		"agentic/rules/b.md": "---\nname: style\ndescription: Another\nalways: true\n---\n\nbody\n",
	}), newFakeProvider())
	if err == nil {
		t.Fatal("resolve: want error for two scanned rules declaring the same name")
	}
	if !strings.Contains(err.Error(), "style") {
		t.Errorf("error = %v, want it to name the duplicated definition", err)
	}
}

// Every category is scanned even after one fails, so a root with several
// broken definitions is reported in one pass rather than one run at a time.
func TestResolve_Scan_ReportsEveryBadCategory(t *testing.T) {
	_, err := resolveEntry(t, makeMapFS(map[string]string{
		entryManifestPath:               entryBody(nil, ""),
		"agentic/skills/tidy/SKILL.md":  "no frontmatter here\n",
		"agentic/agents/scout/AGENT.md": "no frontmatter here either\n",
	}), newFakeProvider())
	if err == nil {
		t.Fatal("resolve: want error")
	}
	for _, want := range []string{"agentic/skills", "agentic/agents"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to name %s", err, want)
		}
	}
}

// ===== context: =====

// rawContextBody is what a consumer's own top-level prose looks like: no
// frontmatter, and content a frontmatter split would mangle.
const rawContextBody = "# This repo\n\nPlain prose, no frontmatter.\n\n---\n\nA horizontal rule, not a delimiter.\n"

func TestResolve_Context_BecomesInstructionNamedContext(t *testing.T) {
	plan, err := resolveEntry(t, makeMapFS(map[string]string{
		entryManifestPath: entryBody(nil, "context: ./CONTEXT.md\n"),
		"CONTEXT.md":      rawContextBody,
	}), newFakeProvider())
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
	if !d.IsContext {
		t.Error("the context instruction is not marked as one, so nothing downstream can tell it apart")
	}
	if d.StackName != "" {
		t.Errorf("stack name = %q, want the entry manifest's own identifier", d.StackName)
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
func TestResolve_Context_NameIsFixedRegardlessOfFilename(t *testing.T) {
	plan, err := resolveEntry(t, makeMapFS(map[string]string{
		entryManifestPath:     entryBody(nil, "context: ./docs/house-style.md\n"),
		"docs/house-style.md": rawContextBody,
	}), newFakeProvider())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Name != "context" {
		t.Fatalf("definitions = %+v, want one named context", plan.Definitions)
	}
}

func TestResolve_Context_RendersVerbatimIntoManagedRegion(t *testing.T) {
	plan, err := resolveEntry(t, makeMapFS(map[string]string{
		entryManifestPath: entryBody(nil, "context: ./CONTEXT.md\n"),
		"CONTEXT.md":      rawContextBody,
	}), newFakeProvider())
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

// A missing category directory is an empty one, but a `context:` the
// manifest names and the repo does not have is a manifest describing content
// that is not there.
func TestResolve_Context_MissingFileRefused(t *testing.T) {
	_, err := resolveEntry(t, makeMapFS(map[string]string{
		entryManifestPath: entryBody(nil, "context: ./CONTEXT.md\n"),
	}), newFakeProvider())
	if err == nil {
		t.Fatal("resolve: want error for a context: file that does not exist")
	}
	for _, want := range []string{"context", "CONTEXT.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to name %s", err, want)
		}
	}
}

// ===== the entry manifest's own content wins =====

// composedProvider serves one stack that lists entries, so scanned content
// has something reached through stacks: to beat.
func composedProvider(files map[string]string) *fakeProvider {
	return newFakeProvider().register("github.com/upstream/base.git", "main", makeMapFS(files))
}

const composedStackRef = "github.com/upstream/base.git/stacks/base.yaml@main"

func TestResolve_Scan_OverridesComposedStackDefinition(t *testing.T) {
	provider := composedProvider(map[string]string{
		"stacks/base.yaml": stackBody(nil, map[string][]string{
			"skills": {"tidy"},
		}),
		"definitions/skills/tidy/SKILL.md": validSkillBody("Upstream tidy"),
	})
	entryFS := makeMapFS(map[string]string{
		entryManifestPath:              entryBody([]string{composedStackRef}, ""),
		"agentic/skills/tidy/SKILL.md": validSkillBody("Local tidy"),
	})

	plan, err := resolveEntry(t, entryFS, provider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %+v, want 1", plan.Definitions)
	}
	d := plan.Definitions[0]
	if d.StackName != "" {
		t.Errorf("winner stack = %q, want the entry manifest's own identifier", d.StackName)
	}
	if got := d.Definition.GetCommon().Description; got != "Local tidy" {
		t.Errorf("description = %q, want the repo's own definition to win", got)
	}
	if got := diagKinds(plan, resolver.DiagOverride); len(got) != 1 {
		t.Errorf("override diagnostics = %v, want one", got)
	}
}

func TestResolve_Scan_NestedCommandOverridesComposedStackDefinition(t *testing.T) {
	provider := composedProvider(map[string]string{
		"stacks/base.yaml": stackBody(nil, map[string][]string{
			"commands": {"foo/bar"},
		}),
		// A namespaced command carries its full name in frontmatter: the
		// filename stem alone is just the last segment.
		"definitions/commands/foo/bar.md": "---\nname: foo/bar\ndescription: Upstream nested command\n---\n\nbody\n",
	})
	entryFS := makeMapFS(map[string]string{
		entryManifestPath:             entryBody([]string{composedStackRef}, ""),
		"agentic/commands/foo/bar.md": validCommandBody("Local nested command"),
	})

	plan, err := resolveEntry(t, entryFS, provider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %+v, want 1", plan.Definitions)
	}
	d := plan.Definitions[0]
	if d.Name != "foo/bar" || d.StackName != "" {
		t.Errorf("winner = (%q, %q), want (foo/bar, the entry manifest)", d.Name, d.StackName)
	}
}

// The fixed name collides with any catalog instruction called "context", and
// the repo's own file wins.
func TestResolve_Context_OverridesComposedStackInstruction(t *testing.T) {
	provider := composedProvider(map[string]string{
		"stacks/base.yaml": stackBody(nil, map[string][]string{
			"instructions": {"context"},
		}),
		"definitions/instructions/context.md": validInstructionBody("Upstream context"),
	})
	entryFS := makeMapFS(map[string]string{
		entryManifestPath: entryBody([]string{composedStackRef}, "context: ./CONTEXT.md\n"),
		"CONTEXT.md":      rawContextBody,
	})

	plan, err := resolveEntry(t, entryFS, provider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %+v, want 1", plan.Definitions)
	}
	if !plan.Definitions[0].IsContext {
		t.Error("the composed stack's instruction won over the repo's own context file")
	}
	if got := diagKinds(plan, resolver.DiagOverride); len(got) != 1 {
		t.Errorf("override diagnostics = %v, want one", got)
	}
}

// A scanned setting has to win the settings merge, not merely the override
// diagnostic. Adapters order contributions by the index of StackName in
// Plan.StackOrder, so the entry manifest's own identifier — the empty string,
// appended last — is what puts the repo's own settings last.
func TestResolve_Scan_SettingWinsRenderedMerge(t *testing.T) {
	provider := composedProvider(map[string]string{
		"stacks/base.yaml": stackBody(nil, map[string][]string{
			"settings": {"zz-upstream-model"},
		}),
		"definitions/settings/zz-upstream-model.yaml": validSettingBody("zz-upstream-model", "from-stack"),
	})
	entryFS := makeMapFS(map[string]string{
		entryManifestPath:                      entryBody([]string{composedStackRef}, ""),
		"agentic/settings/aa-local-model.yaml": validSettingBody("aa-local-model", "from-entry"),
	})

	plan, err := resolveEntry(t, entryFS, provider)
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
	if settings.Model != "from-entry" {
		t.Errorf("model = %q, want from-entry (StackOrder = %v)", settings.Model, plan.StackOrder)
	}
}
