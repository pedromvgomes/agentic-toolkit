package tests

import (
	"errors"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/config"
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/lockfile"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// ===== happy path: local refs only =====

func TestResolve_LocalRefsHappyPath(t *testing.T) {
	primary := makeMapFS(map[string]string{
		"definitions/skills/foo/SKILL.md":  validSkillBody("Skill foo."),
		"definitions/agents/bar/AGENT.md":  validAgentBody("Agent bar."),
		"definitions/rules/baz.md":         validRuleBody("Rule baz."),
		"definitions/instructions/quux.md": validInstructionBody("Instruction quux."),
		"definitions/hooks/h1.yaml":        validHookBody("Hook one."),
		"definitions/presets/default.yaml": validPresetBody("Default bundle.",
			"skills/foo", "agents/bar", "rules/baz", "instructions/quux", "hooks/h1"),
	})

	cfg := &config.ConsumerConfig{
		Source:  config.Source{URL: "primary.example.com/repo", Ref: "main"},
		Presets: []string{"default"},
	}
	provider := newFakeProvider().register("primary.example.com/repo", "main", primary)

	plan, err := resolver.Resolve(cfg, provider)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if len(plan.Definitions) != 5 {
		t.Fatalf("definitions = %d, want 5", len(plan.Definitions))
	}
	// Sorted by (Category, Name).
	want := []struct {
		cat  definitions.Category
		name string
	}{
		{definitions.CategorySkill, "foo"},
		{definitions.CategoryAgent, "bar"},
		{definitions.CategoryRule, "baz"},
		{definitions.CategoryInstruction, "quux"},
		{definitions.CategoryHook, "h1"},
	}
	// The actual category constants are strings; ensure the sort order
	// the resolver picked matches alphabetical of the string values.
	gotNames := make([]string, len(plan.Definitions))
	for i, d := range plan.Definitions {
		gotNames[i] = string(d.Category) + "/" + d.Name
	}
	wantNames := make([]string, len(want))
	for i, w := range want {
		wantNames[i] = string(w.cat) + "/" + w.name
	}
	// Re-sort wantNames the way the resolver would.
	expectSorted := []string{
		string(definitions.CategoryAgent) + "/bar",
		string(definitions.CategoryHook) + "/h1",
		string(definitions.CategoryInstruction) + "/quux",
		string(definitions.CategoryRule) + "/baz",
		string(definitions.CategorySkill) + "/foo",
	}
	if !strSliceEqual(gotNames, expectSorted) {
		t.Errorf("definition order = %v, want %v", gotNames, expectSorted)
	}

	if len(plan.Diagnostics) != 0 {
		t.Errorf("diagnostics = %v, want none", plan.Diagnostics)
	}
	if len(plan.Sources) != 1 {
		t.Fatalf("sources = %d, want 1", len(plan.Sources))
	}
	if plan.Sources[0].Kind != resolver.SourcePrimary {
		t.Errorf("primary kind = %v, want SourcePrimary", plan.Sources[0].Kind)
	}
	if plan.Sources[0].SHA == "" {
		t.Errorf("primary SHA empty")
	}
}

// ===== empty presets: lockfile still includes primary + declared =====

func TestResolve_EmptyPresetsLocksDeclaredSources(t *testing.T) {
	primary := makeMapFS(map[string]string{})
	external := makeMapFS(map[string]string{})

	cfg := &config.ConsumerConfig{
		Source: config.Source{URL: "primary.example.com/repo", Ref: "main"},
		Externals: []config.Source{
			{URL: "ext.example.com/skills", Ref: "v1"},
		},
	}
	provider := newFakeProvider().
		register("primary.example.com/repo", "main", primary).
		register("ext.example.com/skills", "v1", external)

	plan, err := resolver.Resolve(cfg, provider)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(plan.Definitions) != 0 {
		t.Errorf("definitions = %d, want 0", len(plan.Definitions))
	}
	if len(plan.Sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(plan.Sources))
	}
	if plan.Sources[0].Kind != resolver.SourcePrimary || plan.Sources[1].Kind != resolver.SourceDeclared {
		t.Errorf("kinds = [%v, %v], want [primary, declared]", plan.Sources[0].Kind, plan.Sources[1].Kind)
	}
}

// ===== default-branch propagation =====

func TestResolve_DefaultBranchPropagatesIntoPlan(t *testing.T) {
	primary := makeMapFS(map[string]string{})
	cfg := &config.ConsumerConfig{
		Source: config.Source{URL: "primary.example.com/repo"}, // no ref
	}
	// Provider returns Ref="trunk" when asked for empty ref.
	p := newFakeProvider().register("primary.example.com/repo", "", primary)
	// Override the default-branch echo to a non-default name.
	p.entries[fakeKey{URL: "primary.example.com/repo"}] = fakeEntry{
		FS:  primary,
		Ref: "trunk",
		SHA: "deadbeef",
	}

	plan, err := resolver.Resolve(cfg, p)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(plan.Sources) != 1 || plan.Sources[0].Ref != "trunk" {
		t.Errorf("primary ref = %q, want trunk", plan.Sources[0].Ref)
	}
	if plan.Sources[0].SHA != "deadbeef" {
		t.Errorf("primary sha = %q, want deadbeef", plan.Sources[0].SHA)
	}
}

// ===== external skill: implicit classification + diagnostic =====

func TestResolve_ExternalSkillImplicit(t *testing.T) {
	primary := makeMapFS(map[string]string{
		"definitions/presets/default.yaml": validPresetBody("Bundle.",
			"skills::ext.example.com/skills/skill-creator"),
	})
	bundle := makeMapFS(map[string]string{
		"SKILL.md": validSkillBody("External skill."),
	})

	cfg := &config.ConsumerConfig{
		Source:  config.Source{URL: "primary.example.com/repo", Ref: "main"},
		Presets: []string{"default"},
	}
	provider := newFakeProvider().
		register("primary.example.com/repo", "main", primary).
		register("ext.example.com/skills/skill-creator", "", bundle)

	plan, err := resolver.Resolve(cfg, provider)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %d, want 1", len(plan.Definitions))
	}
	d := plan.Definitions[0]
	if d.Category != definitions.CategorySkill || d.Name != "skill-creator" {
		t.Errorf("def = %s/%s, want skill/skill-creator", d.Category, d.Name)
	}
	if d.SourceURL != "ext.example.com/skills/skill-creator" {
		t.Errorf("def SourceURL = %q", d.SourceURL)
	}
	if d.EntryPath != "SKILL.md" {
		t.Errorf("def EntryPath = %q, want SKILL.md", d.EntryPath)
	}

	// Source is in lockfile, classified Implicit.
	var implicit *resolver.PlannedSource
	for i := range plan.Sources {
		if plan.Sources[i].Kind == resolver.SourceImplicit {
			implicit = &plan.Sources[i]
		}
	}
	if implicit == nil {
		t.Fatalf("no implicit source in plan; sources=%v", plan.Sources)
	}
	if implicit.URL != "ext.example.com/skills/skill-creator" {
		t.Errorf("implicit URL = %q", implicit.URL)
	}

	// Diagnostic emitted.
	var found bool
	for _, dg := range plan.Diagnostics {
		if dg.Kind == resolver.DiagImplicitExternal && dg.SourceURL == "ext.example.com/skills/skill-creator" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing DiagImplicitExternal; got %v", plan.Diagnostics)
	}
}

// ===== external skill matching declared external by exact (URL, Ref) =====

func TestResolve_ExternalSkillDeclared(t *testing.T) {
	primary := makeMapFS(map[string]string{
		"definitions/presets/default.yaml": validPresetBody("Bundle.",
			"skills::ext.example.com/skills/foo@main"),
	})
	bundle := makeMapFS(map[string]string{
		"SKILL.md": validSkillBody("Foo skill."),
	})

	cfg := &config.ConsumerConfig{
		Source: config.Source{URL: "primary.example.com/repo", Ref: "main"},
		Externals: []config.Source{
			{URL: "ext.example.com/skills/foo", Ref: "main"},
		},
		Presets: []string{"default"},
	}
	provider := newFakeProvider().
		register("primary.example.com/repo", "main", primary).
		register("ext.example.com/skills/foo", "main", bundle)

	plan, err := resolver.Resolve(cfg, provider)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// No DiagImplicitExternal — the source matched a declared external.
	for _, dg := range plan.Diagnostics {
		if dg.Kind == resolver.DiagImplicitExternal {
			t.Errorf("unexpected implicit diagnostic: %v", dg)
		}
	}
	// Sources: primary + declared (no implicit).
	for _, s := range plan.Sources {
		if s.Kind == resolver.SourceImplicit {
			t.Errorf("unexpected implicit source: %v", s)
		}
	}
}

// ===== override / dedupe =====

func TestResolve_OverrideEmitsDiagnostic(t *testing.T) {
	primary := makeMapFS(map[string]string{
		"definitions/skills/foo/SKILL.md": validSkillBody("Local foo."),
		"definitions/presets/first.yaml":  validPresetBody("First.", "skills/foo"),
		"definitions/presets/second.yaml": validPresetBody("Second.", "skills::ext.example.com/skills/foo"),
	})
	bundle := makeMapFS(map[string]string{
		"SKILL.md": validSkillBody("External foo."),
	})

	cfg := &config.ConsumerConfig{
		Source:  config.Source{URL: "primary.example.com/repo", Ref: "main"},
		Presets: []string{"first", "second"},
	}
	provider := newFakeProvider().
		register("primary.example.com/repo", "main", primary).
		register("ext.example.com/skills/foo", "", bundle)

	plan, err := resolver.Resolve(cfg, provider)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %d, want 1", len(plan.Definitions))
	}
	d := plan.Definitions[0]
	if d.SourceURL != "ext.example.com/skills/foo" {
		t.Errorf("winner SourceURL = %q, want external", d.SourceURL)
	}

	var override *resolver.Diagnostic
	for i := range plan.Diagnostics {
		if plan.Diagnostics[i].Kind == resolver.DiagOverride {
			override = &plan.Diagnostics[i]
		}
	}
	if override == nil {
		t.Fatalf("no DiagOverride in %v", plan.Diagnostics)
	}
	if override.Category != definitions.CategorySkill || override.Name != "foo" {
		t.Errorf("override key = %s/%s, want skill/foo", override.Category, override.Name)
	}
	if override.SourceURL != "primary.example.com/repo" {
		t.Errorf("override SourceURL (loser) = %q, want primary", override.SourceURL)
	}
}

// ===== external non-skill/agent: error =====

func TestResolve_ExternalNonBundleCategoryErrors(t *testing.T) {
	primary := makeMapFS(map[string]string{
		"definitions/presets/default.yaml": validPresetBody("Bundle.",
			"rules::ext.example.com/rules/foo"),
	})
	cfg := &config.ConsumerConfig{
		Source:  config.Source{URL: "primary.example.com/repo", Ref: "main"},
		Presets: []string{"default"},
	}
	provider := newFakeProvider().
		register("primary.example.com/repo", "main", primary)

	_, err := resolver.Resolve(cfg, provider)
	if err == nil {
		t.Fatalf("expected error for external rules ref")
	}
	if !strings.Contains(err.Error(), "external refs are only supported for skills and agents") {
		t.Errorf("error message: %v", err)
	}
}

// ===== error joining =====

func TestResolve_JoinsMultipleEntryErrors(t *testing.T) {
	primary := makeMapFS(map[string]string{
		"definitions/presets/default.yaml": validPresetBody("Bundle.",
			"skills/missing-1", "skills/missing-2"),
	})
	cfg := &config.ConsumerConfig{
		Source:  config.Source{URL: "primary.example.com/repo", Ref: "main"},
		Presets: []string{"default"},
	}
	provider := newFakeProvider().register("primary.example.com/repo", "main", primary)

	_, err := resolver.Resolve(cfg, provider)
	if err == nil {
		t.Fatalf("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "missing-1") || !strings.Contains(msg, "missing-2") {
		t.Errorf("expected both entry errors in joined message; got %q", msg)
	}
}

// ===== primary provider failure is fatal =====

func TestResolve_PrimaryProviderFailureIsFatal(t *testing.T) {
	cfg := &config.ConsumerConfig{
		Source: config.Source{URL: "primary.example.com/repo", Ref: "main"},
	}
	provider := newFakeProvider().fail("primary.example.com/repo", "main", errors.New("network down"))

	_, err := resolver.Resolve(cfg, provider)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "primary source") {
		t.Errorf("error should mention primary; got %q", err)
	}
}

// ===== declared-external provider failure is non-fatal, suppresses cascade =====

func TestResolve_DeclaredExternalFailureSuppressesCascade(t *testing.T) {
	primary := makeMapFS(map[string]string{
		"definitions/presets/default.yaml": validPresetBody("Bundle.",
			"skills::ext.example.com/skills/foo@main"),
	})
	cfg := &config.ConsumerConfig{
		Source: config.Source{URL: "primary.example.com/repo", Ref: "main"},
		Externals: []config.Source{
			{URL: "ext.example.com/skills/foo", Ref: "main"},
		},
		Presets: []string{"default"},
	}
	provider := newFakeProvider().
		register("primary.example.com/repo", "main", primary).
		fail("ext.example.com/skills/foo", "main", errors.New("clone failed"))

	_, err := resolver.Resolve(cfg, provider)
	if err == nil {
		t.Fatalf("expected error from failed declared external")
	}
	msg := err.Error()
	// Provider error present.
	if !strings.Contains(msg, "clone failed") {
		t.Errorf("expected provider error in joined message; got %q", msg)
	}
	// Cascading "missing definition" / "ParseBundle" noise NOT present:
	// the declared-external failure should suppress the per-entry ref
	// processing.
	if strings.Contains(msg, "ParseBundle") || strings.Contains(msg, "SKILL.md") {
		t.Errorf("did not expect cascading parse error; got %q", msg)
	}
}

// ===== source ordering =====

func TestResolve_SourceOrdering(t *testing.T) {
	primary := makeMapFS(map[string]string{
		"definitions/presets/default.yaml": validPresetBody("Bundle.",
			"skills::ext.example.com/skills/zzz",
			"skills::ext.example.com/skills/aaa",
		),
	})
	cfg := &config.ConsumerConfig{
		Source: config.Source{URL: "p.example.com/repo", Ref: "main"},
		Externals: []config.Source{
			{URL: "decl-second.example.com/x", Ref: "v1"},
			{URL: "decl-first.example.com/y", Ref: "v2"},
		},
		Presets: []string{"default"},
	}
	bundle := makeMapFS(map[string]string{"SKILL.md": validSkillBody("z")})

	provider := newFakeProvider().
		register("p.example.com/repo", "main", primary).
		register("decl-second.example.com/x", "v1", makeMapFS(nil)).
		register("decl-first.example.com/y", "v2", makeMapFS(nil)).
		register("ext.example.com/skills/zzz", "", bundle).
		register("ext.example.com/skills/aaa", "", bundle)

	plan, err := resolver.Resolve(cfg, provider)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(plan.Sources) != 5 {
		t.Fatalf("sources = %d, want 5", len(plan.Sources))
	}
	if plan.Sources[0].Kind != resolver.SourcePrimary {
		t.Errorf("[0] kind = %v, want primary", plan.Sources[0].Kind)
	}
	// Declared in cfg.Externals order (NOT alphabetical).
	if plan.Sources[1].URL != "decl-second.example.com/x" {
		t.Errorf("[1] URL = %q, want decl-second.example.com/x", plan.Sources[1].URL)
	}
	if plan.Sources[2].URL != "decl-first.example.com/y" {
		t.Errorf("[2] URL = %q, want decl-first.example.com/y", plan.Sources[2].URL)
	}
	// Implicit, sorted alphabetically by URL.
	if plan.Sources[3].URL != "ext.example.com/skills/aaa" {
		t.Errorf("[3] URL = %q, want ext.example.com/skills/aaa", plan.Sources[3].URL)
	}
	if plan.Sources[4].URL != "ext.example.com/skills/zzz" {
		t.Errorf("[4] URL = %q, want ext.example.com/skills/zzz", plan.Sources[4].URL)
	}
}

// ===== Plan.Lockfile() projection =====

func TestPlan_LockfileProjection(t *testing.T) {
	primary := makeMapFS(map[string]string{})
	cfg := &config.ConsumerConfig{
		Source: config.Source{URL: "primary.example.com/repo", Ref: "main"},
	}
	provider := newFakeProvider().register("primary.example.com/repo", "main", primary)

	plan, err := resolver.Resolve(cfg, provider)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	lf := plan.Lockfile()
	if lf.Version != lockfile.Version {
		t.Errorf("lockfile version = %d, want %d", lf.Version, lockfile.Version)
	}
	if len(lf.Sources) != 1 {
		t.Fatalf("lockfile sources = %d, want 1", len(lf.Sources))
	}
	if lf.Sources[0].URL != "primary.example.com/repo" || lf.Sources[0].Ref != "main" {
		t.Errorf("lockfile source = %+v", lf.Sources[0])
	}
	if lf.Sources[0].SHA == "" {
		t.Errorf("lockfile sha empty")
	}
}

// ===== nil-arg safety =====

func TestResolve_NilArgs(t *testing.T) {
	if _, err := resolver.Resolve(nil, newFakeProvider()); err == nil {
		t.Errorf("expected error for nil cfg")
	}
	if _, err := resolver.Resolve(&config.ConsumerConfig{}, nil); err == nil {
		t.Errorf("expected error for nil provider")
	}
}

// ===== helpers =====

func strSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
