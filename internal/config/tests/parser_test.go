package tests

import (
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/config"
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
)

func TestParse_Shorthand(t *testing.T) {
	cfg, err := config.ParseFile("testdata/valid/shorthand.yaml")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg.Source.URL != "github.com/pedromvgomes/agentic-toolkit" {
		t.Errorf("source.url = %q", cfg.Source.URL)
	}
	if cfg.Source.Ref != "main" {
		t.Errorf("source.ref = %q, want main", cfg.Source.Ref)
	}
	if got, want := cfg.Platforms, []definitions.Platform{definitions.PlatformClaude, definitions.PlatformCursor}; !equalPlatforms(got, want) {
		t.Errorf("platforms = %v, want %v", got, want)
	}
	if len(cfg.Externals) != 2 {
		t.Fatalf("externals = %d, want 2", len(cfg.Externals))
	}
	if cfg.Externals[0].URL != "github.com/anthropics/skills" || cfg.Externals[0].Ref != "main" {
		t.Errorf("externals[0] = %+v", cfg.Externals[0])
	}
	if cfg.Externals[1].URL != "github.com/some/other/repo" || cfg.Externals[1].Ref != "" {
		t.Errorf("externals[1] = %+v", cfg.Externals[1])
	}
	if got, want := cfg.Presets, []string{"default", "bare-repos"}; !equalStrings(got, want) {
		t.Errorf("presets = %v, want %v", got, want)
	}
}

func TestParse_StructForm(t *testing.T) {
	cfg, err := config.ParseFile("testdata/valid/struct-form.yaml")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg.Source.URL != "github.com/pedromvgomes/agentic-toolkit" || cfg.Source.Ref != "main" {
		t.Errorf("source = %+v", cfg.Source)
	}
	if len(cfg.Externals) != 1 || cfg.Externals[0].URL != "github.com/anthropics/skills" || cfg.Externals[0].Ref != "main" {
		t.Errorf("externals = %+v", cfg.Externals)
	}
}

func TestParse_Minimal(t *testing.T) {
	cfg, err := config.ParseFile("testdata/valid/minimal.yaml")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg.Source.URL == "" {
		t.Errorf("source.url is empty")
	}
	if cfg.Source.Ref != "" {
		t.Errorf("source.ref = %q, want empty", cfg.Source.Ref)
	}
	if len(cfg.Platforms) != 0 || len(cfg.Externals) != 0 || len(cfg.Presets) != 0 {
		t.Errorf("expected all optional fields empty, got %+v", cfg)
	}
}

func TestParse_NoRef(t *testing.T) {
	cfg, err := config.ParseFile("testdata/valid/no-ref.yaml")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg.Source.Ref != "" {
		t.Errorf("source.ref = %q, want empty (default branch)", cfg.Source.Ref)
	}
	if len(cfg.Externals) != 1 || cfg.Externals[0].Ref != "" {
		t.Errorf("externals[0].ref = %q, want empty", cfg.Externals[0].Ref)
	}
}

func TestParse_InvalidCases(t *testing.T) {
	cases := []struct {
		name string
		path string
		want config.ErrorKind
	}{
		{"missing-source", "testdata/invalid/missing-source.yaml", config.ErrMissingRequired},
		{"empty-source-shorthand", "testdata/invalid/empty-source-shorthand.yaml", config.ErrYAMLSyntax},
		{"external-empty-url", "testdata/invalid/external-empty-url.yaml", config.ErrInvalidSource},
		{"unknown-platform", "testdata/invalid/unknown-platform.yaml", config.ErrUnknownPlatform},
		{"unknown-field", "testdata/invalid/unknown-field.yaml", config.ErrUnknownField},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.ParseFile(tc.path)
			if err == nil {
				t.Fatalf("expected error of kind %s, got nil", tc.want)
			}
			if !config.IsKind(err, tc.want) {
				t.Errorf("got error kind != %s\n  err: %v", tc.want, err)
			}
		})
	}
}

func equalPlatforms(a, b []definitions.Platform) bool {
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

func equalStrings(a, b []string) bool {
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
