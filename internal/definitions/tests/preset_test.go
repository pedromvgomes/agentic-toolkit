package tests

import (
	"testing"
	"testing/fstest"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
)

// ===== ParsePresetRef =====

func TestParsePresetRef_LocalSimple(t *testing.T) {
	ref, err := definitions.ParsePresetRef("skills/challenge")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ref.IsExternal() {
		t.Errorf("expected local, got external")
	}
	if ref.Category != definitions.CategorySkill {
		t.Errorf("category = %q, want skill", ref.Category)
	}
	if ref.Name != "challenge" {
		t.Errorf("name = %q, want challenge", ref.Name)
	}
}

func TestParsePresetRef_LocalNestedCommand(t *testing.T) {
	ref, err := definitions.ParsePresetRef("commands/git/commit")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ref.Category != definitions.CategoryCommand {
		t.Errorf("category = %q, want command", ref.Category)
	}
	if ref.Name != "git/commit" {
		t.Errorf("name = %q, want git/commit", ref.Name)
	}
}

func TestParsePresetRef_ExternalNoRef(t *testing.T) {
	ref, err := definitions.ParsePresetRef("skills::github.com/anthropics/skills/skills/skill-creator")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ref.IsExternal() {
		t.Errorf("expected external, got local")
	}
	if ref.Category != definitions.CategorySkill {
		t.Errorf("category = %q, want skill", ref.Category)
	}
	if ref.URL != "github.com/anthropics/skills/skills/skill-creator" {
		t.Errorf("url = %q", ref.URL)
	}
	if ref.Ref != "" {
		t.Errorf("ref = %q, want empty", ref.Ref)
	}
}

func TestParsePresetRef_ExternalWithBranch(t *testing.T) {
	ref, err := definitions.ParsePresetRef("skills::github.com/anthropics/skills/skills/skill-creator@main")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ref.URL != "github.com/anthropics/skills/skills/skill-creator" {
		t.Errorf("url = %q", ref.URL)
	}
	if ref.Ref != "main" {
		t.Errorf("ref = %q, want main", ref.Ref)
	}
}

func TestParsePresetRef_ExternalWithTag(t *testing.T) {
	ref, err := definitions.ParsePresetRef("rules::github.com/some/repo/path@v1.2.3")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ref.Category != definitions.CategoryRule {
		t.Errorf("category = %q, want rule", ref.Category)
	}
	if ref.Ref != "v1.2.3" {
		t.Errorf("ref = %q", ref.Ref)
	}
}

func TestParsePresetRef_Errors(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"no-separator", "skill-creator"},
		{"unknown-local-category", "widgets/foo"},
		{"unknown-external-category", "widgets::github.com/x/y/z"},
		{"local-empty-name", "skills/"},
		{"external-empty-url", "skills::"},
		{"external-empty-url-with-ref", "skills::@main"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := definitions.ParsePresetRef(tc.in); err == nil {
				t.Fatalf("expected error for %q", tc.in)
			}
		})
	}
}

// ===== ParsePresetInCatalog =====

func TestParsePreset_Valid(t *testing.T) {
	p, err := definitions.ParsePresetInCatalog(validFS(), "definitions/presets/example.yaml")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if p.Name != "example" {
		t.Errorf("name = %q, want example", p.Name)
	}
	if len(p.Definitions) != 5 {
		t.Errorf("definitions count = %d, want 5", len(p.Definitions))
	}
}

func TestParsePreset_InvalidCases(t *testing.T) {
	cases := []struct {
		name string
		path string
		want definitions.ErrorKind
	}{
		{"missing-description", "definitions/presets/preset-missing-description.yaml", definitions.ErrMissingRequired},
		{"name-mismatch", "definitions/presets/preset-name-mismatch.yaml", definitions.ErrInvalidName},
		{"malformed-ref", "definitions/presets/preset-malformed-ref.yaml", definitions.ErrPresetMalformedRef},
		{"empty-definitions", "definitions/presets/preset-empty-definitions.yaml", definitions.ErrMissingRequired},
		{"unknown-field", "definitions/presets/preset-unknown-field.yaml", definitions.ErrUnknownField},
	}
	fsys := invalidFS()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := definitions.ParsePresetInCatalog(fsys, tc.path)
			if err == nil {
				t.Fatalf("expected error of kind %s, got nil", tc.want)
			}
			if !definitions.IsKind(err, tc.want) {
				t.Errorf("got error kind != %s\n  err: %v", tc.want, err)
			}
		})
	}
}

func TestParsePreset_RejectsNestedPath(t *testing.T) {
	fsys := fstest.MapFS{
		"definitions/presets/sub/x.yaml": &fstest.MapFile{Data: []byte("description: x\ndefinitions:\n  - skills/y\n")},
	}
	_, err := definitions.ParsePresetInCatalog(fsys, "definitions/presets/sub/x.yaml")
	if err == nil {
		t.Fatalf("expected error for nested preset path")
	}
	if !definitions.IsKind(err, definitions.ErrInvalidName) {
		t.Errorf("got %v, want ErrInvalidName", err)
	}
}

func TestParsePreset_RejectsOutsidePresetsDir(t *testing.T) {
	fsys := fstest.MapFS{
		"presets/x.yaml": &fstest.MapFile{Data: []byte("description: x\ndefinitions:\n  - skills/y\n")},
	}
	_, err := definitions.ParsePresetInCatalog(fsys, "presets/x.yaml")
	if err == nil {
		t.Fatalf("expected error for path outside definitions/presets/")
	}
	if !definitions.IsKind(err, definitions.ErrInvalidName) {
		t.Errorf("got %v, want ErrInvalidName", err)
	}
}

// ===== WalkPresets =====

func TestWalkPresets_MissingDirIsNotError(t *testing.T) {
	got, err := definitions.WalkPresets(fstest.MapFS{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestWalkPresets_FindsYAMLFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"definitions/presets/a.yaml": &fstest.MapFile{Data: []byte{}},
		"definitions/presets/b.yml":  &fstest.MapFile{Data: []byte{}},
		"definitions/presets/c.txt":  &fstest.MapFile{Data: []byte{}}, // ignored
	}
	got, err := definitions.WalkPresets(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %v, want 2 entries", got)
	}
}
