package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/codex"
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// TestRender_AllCategories renders one of every category codex handles
// and verifies each lands at its expected path, that AGENTS.md carries
// both the instruction body and the rule's index entry, and that the
// manifest keys every whole-owned path relative to ProjectRoot — the
// deliberate divergence from Claude's single-scope-root manifest, since
// subagents land under .codex/agents/ while skills and rules land under
// .agents/.
func TestRender_AllCategories(t *testing.T) {
	tmp := t.TempDir()

	skillFS := makeFS(map[string]string{
		"definitions/skills/foo/SKILL.md":         "ignored — body comes from def.Body\n",
		"definitions/skills/foo/prompts/extra.md": "extra prompt\n",
	})

	plan := makePlan([]resolver.PlannedDefinition{
		pdSkill("foo", "skill desc", "skill body\n", "definitions/skills/foo", "default", skillFS),
		pdRule("style", "style rule", "style body\n", "default"),
		pdAgent("bar", "agent desc", "agent body\n", "sonnet", "default", nil),
		pdInstruction("plan-approval", "approve plans", "approve plans body", "default"),
	}, "default")

	if err := codex.Render(plan, codex.Options{
		Scope:       codex.ScopeProject,
		ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	expectFile(t, filepath.Join(tmp, ".agents/skills/foo/SKILL.md"), "skill body")
	expectFile(t, filepath.Join(tmp, ".agents/skills/foo/prompts/extra.md"), "extra prompt")
	expectFile(t, filepath.Join(tmp, ".agents/rules/style.md"), "style body")
	expectFile(t, filepath.Join(tmp, ".codex/agents/bar.toml"), "agent body")

	agentsMD := mustRead(t, filepath.Join(tmp, "AGENTS.md"))
	if !strings.Contains(agentsMD, "approve plans body") {
		t.Errorf("AGENTS.md missing instruction body: %q", agentsMD)
	}
	if !strings.Contains(agentsMD, "[style](.agents/rules/style.md)") || !strings.Contains(agentsMD, "style rule") {
		t.Errorf("AGENTS.md missing rule index entry: %q", agentsMD)
	}

	manifest := mustReadJSON(t, filepath.Join(tmp, ".agents/.agtk-manifest.json"))
	files, _ := manifest["files"].(map[string]any)
	for _, want := range []string{
		".agents/skills/foo/SKILL.md",
		".agents/skills/foo/prompts/extra.md",
		".agents/rules/style.md",
		".codex/agents/bar.toml",
		"AGENTS.md",
	} {
		if _, ok := files[want]; !ok {
			t.Errorf("manifest missing %q (got %v)", want, mapKeys(files))
		}
	}
}

// TestRender_SkillFrontmatter_NameAndDescriptionOnly: Codex's documented
// SKILL.md frontmatter is just name and description — no Claude-only
// keys like allowed-tools or argument-hint leak into it.
func TestRender_SkillFrontmatter_NameAndDescriptionOnly(t *testing.T) {
	tmp := t.TempDir()
	fsys := makeFS(map[string]string{"skills/foo/SKILL.md": "ignored\n"})
	plan := makePlan([]resolver.PlannedDefinition{
		pdSkill("foo", "skill desc", "skill body\n", "skills/foo", "default", fsys),
	}, "default")

	if err := codex.Render(plan, codex.Options{Scope: codex.ScopeProject, ProjectRoot: tmp}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	got := mustRead(t, filepath.Join(tmp, ".agents/skills/foo/SKILL.md"))
	if !strings.HasPrefix(got, "---\nname: foo\ndescription: skill desc\n---\n") {
		t.Errorf("unexpected SKILL.md frontmatter: %q", got)
	}
}

// TestRender_AgentTOML_Extensions verifies CodexAgentExt fields render
// into the subagent's TOML alongside the canonical name/description/
// developer_instructions/model.
func TestRender_AgentTOML_Extensions(t *testing.T) {
	tmp := t.TempDir()
	plan := makePlan([]resolver.PlannedDefinition{
		pdAgent("reviewer", "reviews things", "review body\n", "opus", "default", &definitions.CodexAgentExt{
			ModelReasoningEffort: "high",
			SandboxMode:          "workspace-write",
			MCPServers:           []string{"git"},
			SkillsConfig:         map[string]any{"enabled": true},
		}),
	}, "default")

	if err := codex.Render(plan, codex.Options{Scope: codex.ScopeProject, ProjectRoot: tmp}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(tmp, ".codex/agents/reviewer.toml"))
	if err != nil {
		t.Fatalf("read agent toml: %v", err)
	}
	var got map[string]any
	if err := toml.Unmarshal(raw, &got); err != nil {
		t.Fatalf("parse agent toml: %v\n--- got ---\n%s", err, raw)
	}
	if got["name"] != "reviewer" || got["description"] != "reviews things" {
		t.Errorf("unexpected name/description: %v", got)
	}
	if got["developer_instructions"] != "review body\n" {
		t.Errorf("unexpected developer_instructions: %v", got)
	}
	if got["model"] != "opus" {
		t.Errorf("unexpected model: %v", got)
	}
	if got["model_reasoning_effort"] != "high" {
		t.Errorf("unexpected model_reasoning_effort: %v", got)
	}
	if got["sandbox_mode"] != "workspace-write" {
		t.Errorf("unexpected sandbox_mode: %v", got)
	}
}

// TestRender_AgentsMD_OmittedWhenNothingToSay: with no instructions and
// no rules, AGENTS.md is neither written nor tracked.
func TestRender_AgentsMD_OmittedWhenNothingToSay(t *testing.T) {
	tmp := t.TempDir()
	fsys := makeFS(map[string]string{"skills/foo/SKILL.md": "ignored\n"})
	plan := makePlan([]resolver.PlannedDefinition{
		pdSkill("foo", "skill desc", "skill body\n", "skills/foo", "default", fsys),
	}, "default")

	if err := codex.Render(plan, codex.Options{Scope: codex.ScopeProject, ProjectRoot: tmp}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "AGENTS.md")); !os.IsNotExist(err) {
		t.Errorf("AGENTS.md written despite nothing to say (err=%v)", err)
	}
}

// TestRender_Idempotent re-runs the same plan and verifies no errors and
// stable output.
func TestRender_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	plan := simpleProjectPlan()

	for i := range 2 {
		if err := codex.Render(plan, codex.Options{Scope: codex.ScopeProject, ProjectRoot: tmp}); err != nil {
			t.Fatalf("run %d: Render: %v", i, err)
		}
	}
}

// TestRender_DryRun reports planned actions without touching disk.
func TestRender_DryRun(t *testing.T) {
	tmp := t.TempDir()
	plan := simpleProjectPlan()

	var stdout bytes.Buffer
	if err := codex.Render(plan, codex.Options{
		Scope:       codex.ScopeProject,
		ProjectRoot: tmp,
		DryRun:      true,
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("Render dry-run: %v", err)
	}
	if !strings.Contains(stdout.String(), "would write") {
		t.Errorf("dry-run output missing 'would write': %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(tmp, ".agents")); !os.IsNotExist(err) {
		t.Errorf("dry-run created .agents (should not have)")
	}
}

// TestRender_CollisionRefusedWithoutForce: an existing whole-owned file
// not in the manifest blocks the render.
func TestRender_CollisionRefusedWithoutForce(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".agents/rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".agents/rules/style.md"), []byte("user-wrote-this"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := makePlan([]resolver.PlannedDefinition{
		pdRule("style", "style rule", "agtk body\n", "default"),
	}, "default")

	err := codex.Render(plan, codex.Options{Scope: codex.ScopeProject, ProjectRoot: tmp})
	if err == nil {
		t.Fatalf("expected collision error")
	}
	if !strings.Contains(err.Error(), ".agents/rules/style.md") || !strings.Contains(err.Error(), "--force") {
		t.Errorf("error message should name the colliding path and --force: %v", err)
	}
	got := mustRead(t, filepath.Join(tmp, ".agents/rules/style.md"))
	if got != "user-wrote-this" {
		t.Errorf("collided file overwritten despite refusal: %q", got)
	}
}

// TestRender_CollisionForceOverwrites: --force overwrites a colliding
// file and tracks it in the manifest going forward.
func TestRender_CollisionForceOverwrites(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".agents/rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".agents/rules/style.md"), []byte("user-wrote-this"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := makePlan([]resolver.PlannedDefinition{
		pdRule("style", "style rule", "agtk body\n", "default"),
	}, "default")

	if err := codex.Render(plan, codex.Options{Scope: codex.ScopeProject, ProjectRoot: tmp, Force: true}); err != nil {
		t.Fatalf("Render --force: %v", err)
	}

	got := mustRead(t, filepath.Join(tmp, ".agents/rules/style.md"))
	if !strings.Contains(got, "agtk body") {
		t.Errorf("file not overwritten under --force: %q", got)
	}
	manifest := mustReadJSON(t, filepath.Join(tmp, ".agents/.agtk-manifest.json"))
	files, _ := manifest["files"].(map[string]any)
	if _, ok := files[".agents/rules/style.md"]; !ok {
		t.Errorf("manifest should track the forced file: %v", files)
	}
}

// TestRender_StaleCleanup: a rule dropped from the plan is removed, and
// the manifest keys prove RemoveStale reconstructed its path relative to
// ProjectRoot (not to the manifest's own .agents/ directory).
func TestRender_StaleCleanup(t *testing.T) {
	tmp := t.TempDir()

	planA := makePlan([]resolver.PlannedDefinition{
		pdRule("style", "style rule", "agtk body\n", "default"),
		pdRule("naming", "naming rule", "naming body\n", "default"),
	}, "default")
	if err := codex.Render(planA, codex.Options{Scope: codex.ScopeProject, ProjectRoot: tmp}); err != nil {
		t.Fatalf("Render A: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".agents/rules/naming.md")); err != nil {
		t.Fatalf("rules/naming.md not written: %v", err)
	}

	planB := makePlan([]resolver.PlannedDefinition{
		pdRule("style", "style rule", "agtk body\n", "default"),
	}, "default")
	if err := codex.Render(planB, codex.Options{Scope: codex.ScopeProject, ProjectRoot: tmp}); err != nil {
		t.Fatalf("Render B: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".agents/rules/naming.md")); !os.IsNotExist(err) {
		t.Errorf("stale rule file was not removed (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".agents/rules/style.md")); err != nil {
		t.Errorf("kept rule file disappeared: %v", err)
	}
}

// TestRender_StaleCleanup_AGENTSmd: once the last instruction and rule
// disappear from the plan, AGENTS.md — itself just another whole-owned
// file in this manifest — is cleaned up like any other.
func TestRender_StaleCleanup_AGENTSmd(t *testing.T) {
	tmp := t.TempDir()

	planA := makePlan([]resolver.PlannedDefinition{
		pdInstruction("only", "i", "i body", "default"),
	}, "default")
	if err := codex.Render(planA, codex.Options{Scope: codex.ScopeProject, ProjectRoot: tmp}); err != nil {
		t.Fatalf("Render A: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "AGENTS.md")); err != nil {
		t.Fatalf("AGENTS.md not written: %v", err)
	}

	planB := makePlan(nil, "default")
	if err := codex.Render(planB, codex.Options{Scope: codex.ScopeProject, ProjectRoot: tmp}); err != nil {
		t.Fatalf("Render B: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "AGENTS.md")); !os.IsNotExist(err) {
		t.Errorf("stale AGENTS.md was not removed (err=%v)", err)
	}
}

// TestRender_UserScope resolves ProjectRoot from the home directory when
// no override is given.
func TestRender_UserScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	plan := makePlan([]resolver.PlannedDefinition{
		pdInstruction("only-instruction", "i", "i body", "default"),
	}, "default")
	if err := codex.Render(plan, codex.Options{Scope: codex.ScopeUser}); err != nil {
		t.Fatalf("Render user: %v", err)
	}
	got := mustRead(t, filepath.Join(home, "AGENTS.md"))
	if !strings.Contains(got, "i body") {
		t.Errorf("instruction body missing: %q", got)
	}
}

func simpleProjectPlan() *resolver.Plan {
	return makePlan([]resolver.PlannedDefinition{
		pdRule("style", "style rule", "style body\n", "default"),
		pdInstruction("plan-approval", "approve", "approve body", "default"),
	}, "default")
}

func expectFile(t *testing.T, path, wantSubstr string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(got), wantSubstr) {
		t.Errorf("%s missing %q\n--- got ---\n%s", path, wantSubstr, string(got))
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func mustReadJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return m
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
