package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/claude"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// TestRender_AllCategories renders one of every category and verifies
// each lands at its expected path with reasonable contents. The
// .agtk-manifest.json sidecar must list every whole-owned file.
func TestRender_AllCategories(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")

	skillFS := makeFS(map[string]string{
		"definitions/skills/foo/SKILL.md":         "ignored — body comes from def.Body\n",
		"definitions/skills/foo/prompts/extra.md": "extra prompt\n",
		"definitions/skills/foo/schemas/x.json":   `{"x":1}`,
	})
	agentFS := makeFS(map[string]string{
		"definitions/agents/bar/AGENT.md": "ignored — body comes from def.Body\n",
		"definitions/agents/bar/notes.md": "notes\n",
	})

	plan := makePlan([]resolver.PlannedDefinition{
		pdSkill("foo", "skill desc", "skill body\n", "definitions/skills/foo", "default", skillFS),
		pdAgent("bar", "agent desc", "agent body\n", "definitions/agents/bar", "default", agentFS),
		pdCommand("git/commit", "commit cmd", "commit body\n", "default"),
		pdRule("style", "style rule", "style body\n", "default"),
		pdInstruction("plan-approval", "approve plans", "approve plans body", "default"),
		pdHook("safety", "block rm", "PreToolUse", "Bash", "echo blocked", "default"),
		pdMCPStdio("git", "git mcp", "git-mcp", []string{"--repo", "."}, "default"),
		pdSetting("perms", "deny dangerous", map[string]any{
			"permissions": map[string]any{"deny": []any{"Bash(rm -rf:*)"}},
		}, "default"),
	}, "default")

	if err := claude.Render(plan, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Whole-owned files exist.
	expectFile(t, filepath.Join(scopeRoot, "skills/foo/SKILL.md"), "skill body")
	expectFile(t, filepath.Join(scopeRoot, "skills/foo/prompts/extra.md"), "extra prompt")
	expectFile(t, filepath.Join(scopeRoot, "skills/foo/schemas/x.json"), `{"x":1}`)
	expectFile(t, filepath.Join(scopeRoot, "agents/bar/AGENT.md"), "agent body")
	expectFile(t, filepath.Join(scopeRoot, "agents/bar/notes.md"), "notes")
	expectFile(t, filepath.Join(scopeRoot, "commands/git/commit.md"), "commit body")
	expectFile(t, filepath.Join(scopeRoot, "rules/style.md"), "style body")

	// CLAUDE.md exists at project root with managed region.
	claudeMD := mustRead(t, filepath.Join(tmp, "CLAUDE.md"))
	if !strings.Contains(claudeMD, "<!-- BEGIN AGTK MANAGED -->") ||
		!strings.Contains(claudeMD, "<!-- END AGTK MANAGED -->") {
		t.Fatalf("CLAUDE.md missing markers: %q", claudeMD)
	}
	if !strings.Contains(claudeMD, "approve plans body") {
		t.Fatalf("CLAUDE.md missing instruction body: %q", claudeMD)
	}

	// settings.json contains hooks/mcpServers/permissions and the marker.
	settings := mustReadJSON(t, filepath.Join(scopeRoot, "settings.json"))
	for _, key := range []string{"hooks", "mcpServers", "permissions", "_meta"} {
		if _, ok := settings[key]; !ok {
			t.Errorf("settings.json missing top-level key %q (got keys %v)", key, mapKeys(settings))
		}
	}
	managed := managedKeys(t, settings)
	for _, want := range []string{"hooks", "mcpServers", "permissions"} {
		if !contains(managed, want) {
			t.Errorf("_meta.agtk.managed missing %q (got %v)", want, managed)
		}
	}

	// Manifest tracks every whole-owned file and excludes settings.json/CLAUDE.md.
	manifest := mustReadJSON(t, filepath.Join(scopeRoot, ".agtk-manifest.json"))
	files, _ := manifest["files"].(map[string]any)
	for _, want := range []string{
		"skills/foo/SKILL.md",
		"skills/foo/prompts/extra.md",
		"skills/foo/schemas/x.json",
		"agents/bar/AGENT.md",
		"agents/bar/notes.md",
		"commands/git/commit.md",
		"rules/style.md",
	} {
		if _, ok := files[want]; !ok {
			t.Errorf("manifest missing %q (got %v)", want, mapKeys(files))
		}
	}
	if _, leaked := files["settings.json"]; leaked {
		t.Errorf("manifest must not track settings.json")
	}
}

// TestRender_Idempotent re-runs the same plan and verifies no errors;
// the outputs match between runs (manifest content stable, no spurious
// rewrites to settings/CLAUDE.md content).
func TestRender_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")
	plan := simpleProjectPlan()

	for i := 0; i < 2; i++ {
		if err := claude.Render(plan, claude.Options{
			Scope:       claude.ScopeProject,
			ScopeRoot:   scopeRoot,
			ProjectRoot: tmp,
		}); err != nil {
			t.Fatalf("run %d: Render: %v", i, err)
		}
	}

	// Files identical between runs (mtime would differ but we don't
	// assert mtime).
	first := snapshot(t, scopeRoot)
	if err := claude.Render(plan, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("third run: %v", err)
	}
	second := snapshot(t, scopeRoot)
	if !mapEqual(first, second) {
		t.Fatalf("idempotent re-render produced different files:\nfirst: %v\nsecond: %v", first, second)
	}
}

// TestRender_PreservesUserSettingsKeys verifies that an existing
// settings.json with user keys outside the managed list is preserved
// after render.
func TestRender_PreservesUserSettingsKeys(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")
	if err := os.MkdirAll(scopeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-existing user-edited settings.json.
	preExisting := map[string]any{
		"theme":             "dark",
		"editor.fontSize":   14,
		"experimentalThing": map[string]any{"enabled": true},
	}
	writeJSON(t, filepath.Join(scopeRoot, "settings.json"), preExisting)

	plan := makePlan([]resolver.PlannedDefinition{
		pdHook("safety", "block rm", "PreToolUse", "Bash", "echo blocked", "default"),
	}, "default")

	if err := claude.Render(plan, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	settings := mustReadJSON(t, filepath.Join(scopeRoot, "settings.json"))
	if settings["theme"] != "dark" {
		t.Errorf("user key theme lost: got %v", settings["theme"])
	}
	if settings["editor.fontSize"] != float64(14) { // JSON numbers decode as float64
		t.Errorf("user key editor.fontSize lost: got %v", settings["editor.fontSize"])
	}
	if _, ok := settings["hooks"]; !ok {
		t.Errorf("hooks key not written")
	}
}

// TestRender_CLAUDEmd_SeedsFromAGENTSmd: when CLAUDE.md does not exist
// and AGENTS.md does, the seeded CLAUDE.md begins with @AGENTS.md.
func TestRender_CLAUDEmd_SeedsFromAGENTSmd(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")
	if err := os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte("# Project agents\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := makePlan([]resolver.PlannedDefinition{
		pdInstruction("plan-approval", "approve", "approve body", "default"),
	}, "default")

	if err := claude.Render(plan, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	got := mustRead(t, filepath.Join(tmp, "CLAUDE.md"))
	if !strings.HasPrefix(got, "@AGENTS.md\n\n") {
		t.Errorf("CLAUDE.md must start with @AGENTS.md import, got %q", got)
	}
	if !strings.Contains(got, "approve body") {
		t.Errorf("CLAUDE.md missing instruction body: %q", got)
	}
}

// TestRender_CLAUDEmd_SeedsFromAGENTSmd_AtStackDir: bare-repo + worktree
// case. CLAUDE.md is rendered at ProjectRoot (the apply dir), AGENTS.md
// lives next to the manifest at StackDir (a subdir of ProjectRoot). The
// seeded import must be the relative path so the agent can resolve it
// at runtime.
func TestRender_CLAUDEmd_SeedsFromAGENTSmd_AtStackDir(t *testing.T) {
	tmp := t.TempDir()
	stackDir := filepath.Join(tmp, "main")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stackDir, "AGENTS.md"), []byte("# Worktree agents\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scopeRoot := filepath.Join(tmp, ".claude")
	plan := makePlan([]resolver.PlannedDefinition{
		pdInstruction("plan-approval", "approve", "approve body", "default"),
	}, "default")

	if err := claude.Render(plan, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
		StackDir:    stackDir,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	got := mustRead(t, filepath.Join(tmp, "CLAUDE.md"))
	if !strings.HasPrefix(got, "@main/AGENTS.md\n\n") {
		t.Errorf("CLAUDE.md must start with @main/AGENTS.md import, got %q", got)
	}
}

// TestRender_CLAUDEmd_StackDirAGENTSmdWinsOverProjectRoot: when AGENTS.md
// exists at both StackDir and ProjectRoot, StackDir wins (the manifest
// is the project definition).
func TestRender_CLAUDEmd_StackDirAGENTSmdWinsOverProjectRoot(t *testing.T) {
	tmp := t.TempDir()
	stackDir := filepath.Join(tmp, "main")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte("# root\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stackDir, "AGENTS.md"), []byte("# stack\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scopeRoot := filepath.Join(tmp, ".claude")
	plan := makePlan([]resolver.PlannedDefinition{
		pdInstruction("plan-approval", "approve", "approve body", "default"),
	}, "default")

	if err := claude.Render(plan, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
		StackDir:    stackDir,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	got := mustRead(t, filepath.Join(tmp, "CLAUDE.md"))
	if !strings.HasPrefix(got, "@main/AGENTS.md\n\n") {
		t.Errorf("StackDir AGENTS.md should win, got %q", got)
	}
}

// TestRender_CLAUDEmd_PreservesUserContent: an existing CLAUDE.md
// without markers gets the managed block appended; existing content is
// preserved verbatim. A second render replaces only the managed region.
func TestRender_CLAUDEmd_PreservesUserContent(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")
	original := "# My project\n\nCustom instructions for Claude.\n"
	if err := os.WriteFile(filepath.Join(tmp, "CLAUDE.md"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	planA := makePlan([]resolver.PlannedDefinition{
		pdInstruction("rule-a", "rule a", "rule A body", "default"),
	}, "default")
	if err := claude.Render(planA, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render A: %v", err)
	}
	got := mustRead(t, filepath.Join(tmp, "CLAUDE.md"))
	if !strings.HasPrefix(got, original) {
		t.Errorf("user content not preserved at top: %q", got)
	}
	if !strings.Contains(got, "rule A body") {
		t.Errorf("missing rule A body: %q", got)
	}

	// Second render with a different instruction replaces only the
	// region — user prefix is unchanged.
	planB := makePlan([]resolver.PlannedDefinition{
		pdInstruction("rule-b", "rule b", "rule B body", "default"),
	}, "default")
	if err := claude.Render(planB, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render B: %v", err)
	}
	got = mustRead(t, filepath.Join(tmp, "CLAUDE.md"))
	if !strings.HasPrefix(got, original) {
		t.Errorf("user content not preserved on re-render: %q", got)
	}
	if strings.Contains(got, "rule A body") {
		t.Errorf("rule A body not replaced on re-render: %q", got)
	}
	if !strings.Contains(got, "rule B body") {
		t.Errorf("rule B body missing on re-render: %q", got)
	}
}

// TestRender_DryRun does no writes but reports actions.
func TestRender_DryRun(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")
	plan := simpleProjectPlan()

	var stdout bytes.Buffer
	if err := claude.Render(plan, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
		DryRun:      true,
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("Render dry-run: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "would write") {
		t.Errorf("dry-run output missing 'would write': %q", out)
	}
	if _, err := os.Stat(scopeRoot); !os.IsNotExist(err) {
		t.Errorf("dry-run created %s (should not have)", scopeRoot)
	}
	if _, err := os.Stat(filepath.Join(tmp, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Errorf("dry-run wrote CLAUDE.md")
	}
}

// TestRender_CollisionRefusedWithoutForce: an existing whole-owned file
// not in the manifest blocks the render.
func TestRender_CollisionRefusedWithoutForce(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")
	if err := os.MkdirAll(filepath.Join(scopeRoot, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-existing file the user wrote — not tracked in manifest.
	if err := os.WriteFile(filepath.Join(scopeRoot, "rules/style.md"), []byte("user-wrote-this"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := makePlan([]resolver.PlannedDefinition{
		pdRule("style", "style rule", "agtk body\n", "default"),
	}, "default")

	err := claude.Render(plan, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	})
	if err == nil {
		t.Fatalf("expected collision error")
	}
	if !strings.Contains(err.Error(), "rules/style.md") || !strings.Contains(err.Error(), "--force") {
		t.Errorf("error message should name the colliding path and --force: %v", err)
	}
	// File must remain untouched.
	got := mustRead(t, filepath.Join(scopeRoot, "rules/style.md"))
	if got != "user-wrote-this" {
		t.Errorf("collided file overwritten despite refusal: %q", got)
	}
}

// TestRender_CollisionForceOverwrites: --force overwrites a colliding
// file and tracks it in the manifest going forward.
func TestRender_CollisionForceOverwrites(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")
	if err := os.MkdirAll(filepath.Join(scopeRoot, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scopeRoot, "rules/style.md"), []byte("user-wrote-this"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := makePlan([]resolver.PlannedDefinition{
		pdRule("style", "style rule", "agtk body\n", "default"),
	}, "default")

	if err := claude.Render(plan, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
		Force:       true,
	}); err != nil {
		t.Fatalf("Render --force: %v", err)
	}

	got := mustRead(t, filepath.Join(scopeRoot, "rules/style.md"))
	if !strings.Contains(got, "agtk body") {
		t.Errorf("file not overwritten under --force: %q", got)
	}
	manifest := mustReadJSON(t, filepath.Join(scopeRoot, ".agtk-manifest.json"))
	files, _ := manifest["files"].(map[string]any)
	if _, ok := files["rules/style.md"]; !ok {
		t.Errorf("manifest should track the forced file: %v", files)
	}
}

// TestRender_StaleCleanup: a file that was tracked in the previous
// manifest but is not in the new plan gets removed.
func TestRender_StaleCleanup(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")

	planA := makePlan([]resolver.PlannedDefinition{
		pdRule("style", "style rule", "agtk body\n", "default"),
		pdRule("naming", "naming rule", "naming body\n", "default"),
	}, "default")
	if err := claude.Render(planA, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render A: %v", err)
	}
	if _, err := os.Stat(filepath.Join(scopeRoot, "rules/naming.md")); err != nil {
		t.Fatalf("rules/naming.md not written: %v", err)
	}

	planB := makePlan([]resolver.PlannedDefinition{
		pdRule("style", "style rule", "agtk body\n", "default"),
	}, "default")
	if err := claude.Render(planB, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render B: %v", err)
	}
	if _, err := os.Stat(filepath.Join(scopeRoot, "rules/naming.md")); !os.IsNotExist(err) {
		t.Errorf("stale rule file was not removed (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(scopeRoot, "rules/style.md")); err != nil {
		t.Errorf("kept rule file disappeared: %v", err)
	}
}

// TestRender_UserScope writes under ScopeRoot directly with no
// AGENTS.md fallback.
func TestRender_UserScope(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, "user-claude")
	// AGENTS.md exists at the equivalent project location, but user
	// scope must not consult it.
	if err := os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte("# unused\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := makePlan([]resolver.PlannedDefinition{
		pdInstruction("only-instruction", "i", "i body", "default"),
	}, "default")
	if err := claude.Render(plan, claude.Options{
		Scope:     claude.ScopeUser,
		ScopeRoot: scopeRoot,
	}); err != nil {
		t.Fatalf("Render user: %v", err)
	}
	got := mustRead(t, filepath.Join(scopeRoot, "CLAUDE.md"))
	if strings.Contains(got, "@AGENTS.md") {
		t.Errorf("user scope should not seed @AGENTS.md, got %q", got)
	}
	if !strings.Contains(got, "i body") {
		t.Errorf("instruction body missing: %q", got)
	}
}

// TestRender_SettingPresetOrder: when two settings touch the same
// top-level key, the later preset wins.
func TestRender_SettingPresetOrder(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")

	plan := makePlan([]resolver.PlannedDefinition{
		pdSetting("a", "low precedence", map[string]any{
			"env": map[string]any{"FOO": "from-low"},
		}, "low"),
		pdSetting("b", "high precedence", map[string]any{
			"env": map[string]any{"FOO": "from-high"},
		}, "high"),
	}, "low", "high") // preset order: low before high

	if err := claude.Render(plan, claude.Options{
		Scope:       claude.ScopeProject,
		ScopeRoot:   scopeRoot,
		ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	settings := mustReadJSON(t, filepath.Join(scopeRoot, "settings.json"))
	envBlock, ok := settings["env"].(map[string]any)
	if !ok {
		t.Fatalf("env block missing: %v", settings)
	}
	if envBlock["FOO"] != "from-high" {
		t.Errorf("expected FOO=from-high (later preset wins), got %v", envBlock["FOO"])
	}
}

// ===== test fixtures =====

func simpleProjectPlan() *resolver.Plan {
	return makePlan([]resolver.PlannedDefinition{
		pdRule("style", "style rule", "style body\n", "default"),
		pdInstruction("plan-approval", "approve", "approve body", "default"),
		pdHook("safety", "block rm", "PreToolUse", "Bash", "echo blocked", "default"),
	}, "default")
}

// ===== assertion helpers =====

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

func writeJSON(t *testing.T, path string, m map[string]any) {
	t.Helper()
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func managedKeys(t *testing.T, settings map[string]any) []string {
	t.Helper()
	meta, _ := settings["_meta"].(map[string]any)
	if meta == nil {
		return nil
	}
	agtk, _ := meta["agtk"].(map[string]any)
	raw, _ := agtk["managed"].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// snapshot reads every file under root into a map keyed by relative
// path. Used to assert idempotency.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
