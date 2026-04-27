package definitions

import (
	"testing"
)

// ===== ParsePresetRef =====

func TestParsePresetRef_LocalSimple(t *testing.T) {
	ref, err := ParsePresetRef("skills/challenge")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ref.IsExternal() {
		t.Errorf("expected local, got external")
	}
	if ref.Category != CategorySkill {
		t.Errorf("category = %q, want skill", ref.Category)
	}
	if ref.Name != "challenge" {
		t.Errorf("name = %q, want challenge", ref.Name)
	}
}

func TestParsePresetRef_LocalNestedCommand(t *testing.T) {
	ref, err := ParsePresetRef("commands/git/commit")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ref.Category != CategoryCommand {
		t.Errorf("category = %q, want command", ref.Category)
	}
	if ref.Name != "git/commit" {
		t.Errorf("name = %q, want git/commit", ref.Name)
	}
}

func TestParsePresetRef_ExternalNoRef(t *testing.T) {
	ref, err := ParsePresetRef("skills::github.com/anthropics/skills/skills/skill-creator")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ref.IsExternal() {
		t.Errorf("expected external, got local")
	}
	if ref.Category != CategorySkill {
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
	ref, err := ParsePresetRef("skills::github.com/anthropics/skills/skills/skill-creator@main")
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
	ref, err := ParsePresetRef("rules::github.com/some/repo/path@v1.2.3")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ref.Category != CategoryRule {
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
			if _, err := ParsePresetRef(tc.in); err == nil {
				t.Fatalf("expected error for %q", tc.in)
			}
		})
	}
}

// ===== ParsePresetFile =====

func TestParsePreset_Valid(t *testing.T) {
	p, err := ParsePresetFile("testdata/valid/presets/example.yaml")
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
		want ErrorKind
	}{
		{"missing-description", "testdata/invalid/preset-missing-description.yaml", ErrMissingRequired},
		{"name-mismatch", "testdata/invalid/preset-name-mismatch.yaml", ErrInvalidName},
		{"malformed-ref", "testdata/invalid/preset-malformed-ref.yaml", ErrPresetMalformedRef},
		{"empty-definitions", "testdata/invalid/preset-empty-definitions.yaml", ErrMissingRequired},
		{"unknown-field", "testdata/invalid/preset-unknown-field.yaml", ErrUnknownField},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePresetFile(tc.path)
			if err == nil {
				t.Fatalf("expected error of kind %s, got nil", tc.want)
			}
			if !IsKind(err, tc.want) {
				t.Errorf("got error kind != %s\n  err: %v", tc.want, err)
			}
		})
	}
}

// ===== WalkPresets =====

func TestWalkPresets_MissingDirIsNotError(t *testing.T) {
	got, err := WalkPresets("testdata") // testdata/definitions/presets does not exist
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}
