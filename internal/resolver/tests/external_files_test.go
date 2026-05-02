package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/config"
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// ===== external rule: implicit, name from frontmatter via stem fallback =====

func TestResolve_ExternalRuleImplicit(t *testing.T) {
	primary := makeMapFS(map[string]string{
		"definitions/presets/default.yaml": validPresetBody("Bundle.",
			"rules::ext.example.com/repo.git/path/to/style.md"),
	})
	parent := makeMapFS(map[string]string{
		"style.md": validRuleBody("External rule."),
	})

	cfg := &config.ConsumerConfig{
		Source:  config.Source{URL: "primary.example.com/repo", Ref: "main"},
		Presets: []string{"default"},
	}
	provider := newFakeProvider().
		register("primary.example.com/repo", "main", primary).
		register("ext.example.com/repo.git/path/to", "", parent)

	plan, err := resolver.Resolve(cfg, provider)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %d, want 1", len(plan.Definitions))
	}
	d := plan.Definitions[0]
	if d.Category != definitions.CategoryRule || d.Name != "style" {
		t.Errorf("def = %s/%s, want rule/style", d.Category, d.Name)
	}
	if d.SourceURL != "ext.example.com/repo.git/path/to/style.md" {
		t.Errorf("SourceURL = %q (want full file URL per D4β)", d.SourceURL)
	}
	if d.EntryPath != "style.md" {
		t.Errorf("EntryPath = %q, want style.md", d.EntryPath)
	}

	// Lockfile pin uses the file URL.
	var implicit *resolver.PlannedSource
	for i := range plan.Sources {
		if plan.Sources[i].Kind == resolver.SourceImplicit {
			implicit = &plan.Sources[i]
		}
	}
	if implicit == nil {
		t.Fatalf("no implicit source; sources=%v", plan.Sources)
	}
	if implicit.URL != "ext.example.com/repo.git/path/to/style.md" {
		t.Errorf("implicit URL = %q (want file URL)", implicit.URL)
	}
}

// ===== external command with nested name from frontmatter =====

func TestResolve_ExternalCommandNestedName(t *testing.T) {
	primary := makeMapFS(map[string]string{
		"definitions/presets/default.yaml": validPresetBody("Bundle.",
			"commands::ext.example.com/repo.git/cmd/commit.md"),
	})
	parent := makeMapFS(map[string]string{
		"commit.md": "---\n" +
			"name: git/commit\n" +
			"description: Commit changes.\n" +
			"---\n\nbody\n",
	})

	cfg := &config.ConsumerConfig{
		Source:  config.Source{URL: "primary.example.com/repo", Ref: "main"},
		Presets: []string{"default"},
	}
	provider := newFakeProvider().
		register("primary.example.com/repo", "main", primary).
		register("ext.example.com/repo.git/cmd", "", parent)

	plan, err := resolver.Resolve(cfg, provider)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %d, want 1", len(plan.Definitions))
	}
	d := plan.Definitions[0]
	if d.Category != definitions.CategoryCommand || d.Name != "git/commit" {
		t.Errorf("def = %s/%s, want command/git/commit", d.Category, d.Name)
	}
}

// ===== external hook (.yaml) =====

func TestResolve_ExternalHookYAML(t *testing.T) {
	primary := makeMapFS(map[string]string{
		"definitions/presets/default.yaml": validPresetBody("Bundle.",
			"hooks::ext.example.com/repo.git/hooks/triage.yaml"),
	})
	parent := makeMapFS(map[string]string{
		"triage.yaml": validHookBody("Triage hook."),
	})

	cfg := &config.ConsumerConfig{
		Source:  config.Source{URL: "primary.example.com/repo", Ref: "main"},
		Presets: []string{"default"},
	}
	provider := newFakeProvider().
		register("primary.example.com/repo", "main", primary).
		register("ext.example.com/repo.git/hooks", "", parent)

	plan, err := resolver.Resolve(cfg, provider)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %d, want 1", len(plan.Definitions))
	}
	d := plan.Definitions[0]
	if d.Category != definitions.CategoryHook || d.Name != "triage" {
		t.Errorf("def = %s/%s, want hook/triage", d.Category, d.Name)
	}
}

// ===== external setting (.yaml) =====

func TestResolve_ExternalSettingYAML(t *testing.T) {
	primary := makeMapFS(map[string]string{
		"definitions/presets/default.yaml": validPresetBody("Bundle.",
			"settings::ext.example.com/repo.git/settings/deny.yaml"),
	})
	parent := makeMapFS(map[string]string{
		"deny.yaml": validSettingBody("Deny dangerous shell."),
	})

	cfg := &config.ConsumerConfig{
		Source:  config.Source{URL: "primary.example.com/repo", Ref: "main"},
		Presets: []string{"default"},
	}
	provider := newFakeProvider().
		register("primary.example.com/repo", "main", primary).
		register("ext.example.com/repo.git/settings", "", parent)

	plan, err := resolver.Resolve(cfg, provider)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %d, want 1", len(plan.Definitions))
	}
	d := plan.Definitions[0]
	if d.Category != definitions.CategorySetting || d.Name != "deny" {
		t.Errorf("def = %s/%s, want setting/deny", d.Category, d.Name)
	}
	if d.SourceURL != "ext.example.com/repo.git/settings/deny.yaml" {
		t.Errorf("SourceURL = %q (want full file URL)", d.SourceURL)
	}
}

// ===== two file refs in same repo+ref → two source entries (D4β) =====

func TestResolve_MultipleFileRefsSameRepoTwoSources(t *testing.T) {
	primary := makeMapFS(map[string]string{
		"definitions/presets/default.yaml": validPresetBody("Bundle.",
			"rules::ext.example.com/repo.git/rules/a.md",
			"rules::ext.example.com/repo.git/rules/b.md"),
	})
	parent := makeMapFS(map[string]string{
		"a.md": validRuleBody("Rule a."),
		"b.md": validRuleBody("Rule b."),
	})

	cfg := &config.ConsumerConfig{
		Source:  config.Source{URL: "primary.example.com/repo", Ref: "main"},
		Presets: []string{"default"},
	}
	provider := newFakeProvider().
		register("primary.example.com/repo", "main", primary).
		register("ext.example.com/repo.git/rules", "", parent)

	plan, err := resolver.Resolve(cfg, provider)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(plan.Definitions) != 2 {
		t.Fatalf("definitions = %d, want 2", len(plan.Definitions))
	}

	// Two distinct implicit source entries, one per file URL.
	var implicit []resolver.PlannedSource
	for _, s := range plan.Sources {
		if s.Kind == resolver.SourceImplicit {
			implicit = append(implicit, s)
		}
	}
	if len(implicit) != 2 {
		t.Fatalf("implicit sources = %d, want 2; got %v", len(implicit), implicit)
	}
	urls := []string{implicit[0].URL, implicit[1].URL}
	wantA := "ext.example.com/repo.git/rules/a.md"
	wantB := "ext.example.com/repo.git/rules/b.md"
	if !((urls[0] == wantA && urls[1] == wantB) || (urls[0] == wantB && urls[1] == wantA)) {
		t.Errorf("implicit URLs = %v, want both %q and %q", urls, wantA, wantB)
	}
}

// ===== malformed external file ref: URL has no in-repo path after .git/ =====

func TestResolve_ExternalFileRefMissingFilenameErrors(t *testing.T) {
	primary := makeMapFS(map[string]string{
		"definitions/presets/default.yaml": validPresetBody("Bundle.",
			"rules::ext.example.com/repo.git"),
	})

	cfg := &config.ConsumerConfig{
		Source:  config.Source{URL: "primary.example.com/repo", Ref: "main"},
		Presets: []string{"default"},
	}
	provider := newFakeProvider().register("primary.example.com/repo", "main", primary)

	_, err := resolver.Resolve(cfg, provider)
	if err == nil {
		t.Fatalf("expected error for file ref missing in-repo path")
	}
	if !strings.Contains(err.Error(), "must include a filename") {
		t.Errorf("error = %v, want filename error", err)
	}
}

// ===== external file ref author tries to nest a non-command name → reject =====

func TestResolve_ExternalRuleRejectsNestedName(t *testing.T) {
	primary := makeMapFS(map[string]string{
		"definitions/presets/default.yaml": validPresetBody("Bundle.",
			"rules::ext.example.com/repo.git/r.md"),
	})
	parent := makeMapFS(map[string]string{
		"r.md": "---\n" +
			"name: group/foo\n" +
			"description: Bad.\n" +
			"---\n\n",
	})

	cfg := &config.ConsumerConfig{
		Source:  config.Source{URL: "primary.example.com/repo", Ref: "main"},
		Presets: []string{"default"},
	}
	provider := newFakeProvider().
		register("primary.example.com/repo", "main", primary).
		register("ext.example.com/repo.git", "", parent)

	_, err := resolver.Resolve(cfg, provider)
	if err == nil {
		t.Fatalf("expected error for nested rule name")
	}
	if !strings.Contains(err.Error(), "must be flat") {
		t.Errorf("error = %v, want flat-name error", err)
	}
}

// ===== mixed bundle + file external refs in a single preset =====

func TestResolve_MixedBundleAndFileExternals(t *testing.T) {
	primary := makeMapFS(map[string]string{
		"definitions/presets/default.yaml": validPresetBody("Mixed.",
			"skills::ext.example.com/skills/foo",
			"rules::ext.example.com/repo.git/style.md"),
	})
	bundle := makeMapFS(map[string]string{
		"SKILL.md": validSkillBody("Foo skill."),
	})
	ruleParent := makeMapFS(map[string]string{
		"style.md": validRuleBody("Style rule."),
	})

	cfg := &config.ConsumerConfig{
		Source:  config.Source{URL: "primary.example.com/repo", Ref: "main"},
		Presets: []string{"default"},
	}
	provider := newFakeProvider().
		register("primary.example.com/repo", "main", primary).
		register("ext.example.com/skills/foo", "", bundle).
		register("ext.example.com/repo.git", "", ruleParent)

	plan, err := resolver.Resolve(cfg, provider)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(plan.Definitions) != 2 {
		t.Fatalf("definitions = %d, want 2", len(plan.Definitions))
	}

	// Both lock entries present, both implicit, with their respective URLs.
	urls := map[string]bool{}
	for _, s := range plan.Sources {
		if s.Kind == resolver.SourceImplicit {
			urls[s.URL] = true
		}
	}
	if !urls["ext.example.com/skills/foo"] {
		t.Errorf("missing skill source URL; got %v", urls)
	}
	if !urls["ext.example.com/repo.git/style.md"] {
		t.Errorf("missing rule source URL; got %v", urls)
	}
}
