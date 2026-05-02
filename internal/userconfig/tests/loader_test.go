package tests

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/userconfig"
)

func TestLoad_DefaultsWhenMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := userconfig.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := userconfig.Default()
	if cfg != want {
		t.Errorf("Load() = %+v, want %+v", cfg, want)
	}
}

func TestLoadFrom_RealFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	contents := "auto_update:\n  enabled: false\n  check_interval: 1h\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := userconfig.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.AutoUpdate.Enabled {
		t.Errorf("Enabled should be false")
	}
	if cfg.AutoUpdate.CheckInterval != time.Hour {
		t.Errorf("CheckInterval = %v, want 1h", cfg.AutoUpdate.CheckInterval)
	}
}

func TestLoadFrom_RejectsUnknownKeys(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	contents := "auto_update:\n  enabled: true\n  garbage: value\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := userconfig.LoadFrom(path); err == nil {
		t.Fatal("expected error on unknown field")
	}
}
