package tests

import (
	"os"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/config"
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// TestResolve_Integration_DirFS exercises the resolver end-to-end over
// real on-disk fixtures laid out the way a real toolkit catalog would
// be: testdata/primary/definitions/... is a primary source mirror, and
// testdata/external/skill-creator/ is an external bundle (rooted at the
// skill folder itself).
func TestResolve_Integration_DirFS(t *testing.T) {
	primary := os.DirFS("testdata/primary")
	bundle := os.DirFS("testdata/external/skill-creator")

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

	wantDefs := map[string]definitions.Category{
		"skill-creator": definitions.CategorySkill,
		"challenge":     definitions.CategorySkill,
		"plan-approval": definitions.CategoryInstruction,
	}
	if len(plan.Definitions) != len(wantDefs) {
		t.Fatalf("definitions = %d, want %d (%v)", len(plan.Definitions), len(wantDefs), plan.Definitions)
	}
	for _, d := range plan.Definitions {
		want, ok := wantDefs[d.Name]
		if !ok {
			t.Errorf("unexpected definition %s/%s", d.Category, d.Name)
			continue
		}
		if d.Category != want {
			t.Errorf("%s: category = %s, want %s", d.Name, d.Category, want)
		}
	}

	// External skill should classify as implicit and surface as a
	// diagnostic.
	var sawImplicit bool
	for _, dg := range plan.Diagnostics {
		if dg.Kind == resolver.DiagImplicitExternal && dg.SourceURL == "ext.example.com/skills/skill-creator" {
			sawImplicit = true
		}
	}
	if !sawImplicit {
		t.Errorf("expected DiagImplicitExternal for external skill; got %v", plan.Diagnostics)
	}

	// Lockfile carries primary + implicit external.
	lf := plan.Lockfile()
	if len(lf.Sources) != 2 {
		t.Fatalf("lockfile sources = %d, want 2 (%v)", len(lf.Sources), lf.Sources)
	}
	if lf.Sources[0].URL != "primary.example.com/repo" {
		t.Errorf("lockfile[0] = %+v, want primary first", lf.Sources[0])
	}
}
