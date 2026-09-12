package tests

import (
	"testing"
	"testing/fstest"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
)

func TestParse_ValidSetting(t *testing.T) {
	def, err := definitions.ParseInCatalog(validFS(), "definitions/settings/deny-dangerous-shell.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := def.(*definitions.Setting)
	if !ok {
		t.Fatalf("got %T, want *Setting", def)
	}
	if s.Name != "deny-dangerous-shell" {
		t.Errorf("name = %q, want deny-dangerous-shell", s.Name)
	}
	if def.Category() != definitions.CategorySetting {
		t.Errorf("category = %q, want %q", def.Category(), definitions.CategorySetting)
	}
	perms, ok := s.Value["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("value.permissions not parsed as map: %T (%v)", s.Value["permissions"], s.Value)
	}
	deny, ok := perms["deny"].([]any)
	if !ok || len(deny) != 2 {
		t.Errorf("value.permissions.deny = %v, want 2-element list", perms["deny"])
	}
	env, ok := s.Value["env"].(map[string]any)
	if !ok || env["AGTK_DEBUG"] != "1" {
		t.Errorf("value.env = %v, want {AGTK_DEBUG: '1'}", s.Value["env"])
	}
}

func TestParse_SettingEmptyValue(t *testing.T) {
	_, err := definitions.ParseInCatalog(invalidFS(), "definitions/settings/empty-value.yaml")
	if err == nil {
		t.Fatalf("expected error for empty value")
	}
	if !definitions.IsKind(err, definitions.ErrMissingRequired) {
		t.Errorf("got %v, want ErrMissingRequired", err)
	}
}

func TestParse_SettingMissingValue(t *testing.T) {
	_, err := definitions.ParseInCatalog(invalidFS(), "definitions/settings/missing-value.yaml")
	if err == nil {
		t.Fatalf("expected error for missing value")
	}
	if !definitions.IsKind(err, definitions.ErrMissingRequired) {
		t.Errorf("got %v, want ErrMissingRequired", err)
	}
}

// ParseFile is the entry point for external file refs (settings::repo.git/path/file.yaml).
// Layout-agnostic: filename derives the canonical name when frontmatter omits it.
func TestParseFile_SettingNameFromFilenameStem(t *testing.T) {
	fsys := fstest.MapFS{
		"theme.yaml": &fstest.MapFile{Data: []byte("" +
			"description: Theme override.\n" +
			"value:\n" +
			"  theme: dark\n",
		)},
	}
	def, err := definitions.ParseFile(fsys, definitions.CategorySetting, "theme.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := def.(*definitions.Setting)
	if s.Name != "theme" {
		t.Errorf("name = %q, want stem fallback 'theme'", s.Name)
	}
	if s.Value["theme"] != "dark" {
		t.Errorf("value.theme = %v, want 'dark'", s.Value["theme"])
	}
}

func TestParseFile_SettingNameFromFrontmatter(t *testing.T) {
	fsys := fstest.MapFS{
		"any-name.yaml": &fstest.MapFile{Data: []byte("" +
			"name: explicit-name\n" +
			"description: Setting with explicit name.\n" +
			"value:\n" +
			"  flag: true\n",
		)},
	}
	def, err := definitions.ParseFile(fsys, definitions.CategorySetting, "any-name.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.GetCommon().Name != "explicit-name" {
		t.Errorf("name = %q, want frontmatter wins", def.GetCommon().Name)
	}
}

func TestParseFile_SettingRejectsNestedName(t *testing.T) {
	fsys := fstest.MapFS{
		"x.yaml": &fstest.MapFile{Data: []byte("" +
			"name: a/b\n" +
			"description: Nested name.\n" +
			"value:\n" +
			"  k: v\n",
		)},
	}
	_, err := definitions.ParseFile(fsys, definitions.CategorySetting, "x.yaml")
	if err == nil {
		t.Fatalf("expected error for nested name on setting")
	}
	if !definitions.IsKind(err, definitions.ErrInvalidName) {
		t.Errorf("got %v, want ErrInvalidName", err)
	}
}

func TestSetting_Categories_IncludesSetting(t *testing.T) {
	for _, c := range definitions.AllCategories {
		if c == definitions.CategorySetting {
			return
		}
	}
	t.Fatalf("CategorySetting not in AllCategories: %v", definitions.AllCategories)
}

func TestSetting_CategoryDir(t *testing.T) {
	if got := definitions.CategorySetting.CategoryDir(); got != "settings" {
		t.Errorf("CategoryDir() = %q, want 'settings'", got)
	}
	if got := definitions.CategoryFromDir("settings"); got != definitions.CategorySetting {
		t.Errorf("CategoryFromDir('settings') = %q, want CategorySetting", got)
	}
}
