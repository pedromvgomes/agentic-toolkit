package tests

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInit_WritesScaffold(t *testing.T) {
	work := t.TempDir()
	stdout, _, err := runCLI(t, work, "init")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	configPath := filepath.Join(work, ".agentic-toolkit.yaml")
	body := readFile(t, configPath)
	if !strings.Contains(stdout, configPath) {
		t.Errorf("stdout should mention written path: %q", stdout)
	}
	if !strings.Contains(body, "TODO") {
		t.Errorf("default scaffold should carry a TODO placeholder; got:\n%s", body)
	}
	if !strings.Contains(body, "presets:") || !strings.Contains(body, "- default") {
		t.Errorf("scaffold should seed presets list; got:\n%s", body)
	}
}

func TestInit_WithSourceFlag_SeedsURL(t *testing.T) {
	work := t.TempDir()
	_, _, err := runCLI(t, work, "init", "--source", "github.com/owner/repo")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	body := readFile(t, filepath.Join(work, ".agentic-toolkit.yaml"))
	if !strings.Contains(body, "source: github.com/owner/repo") {
		t.Errorf("--source should set the source line; got:\n%s", body)
	}
	if strings.Contains(body, "TODO") {
		t.Errorf("explicit --source should suppress TODO placeholder; got:\n%s", body)
	}
}

func TestInit_RefusesOverwrite_WithoutForce(t *testing.T) {
	work := t.TempDir()
	configPath := filepath.Join(work, ".agentic-toolkit.yaml")
	writeFile(t, configPath, "existing: content\n")

	_, _, err := runCLI(t, work, "init")
	if err == nil {
		t.Fatal("expected error when file exists without --force")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should explain the conflict: %v", err)
	}
	// File should be untouched.
	if got := readFile(t, configPath); got != "existing: content\n" {
		t.Errorf("file was modified without --force: %q", got)
	}
}

func TestInit_OverwritesWithForce(t *testing.T) {
	work := t.TempDir()
	configPath := filepath.Join(work, ".agentic-toolkit.yaml")
	writeFile(t, configPath, "existing: content\n")

	if _, _, err := runCLI(t, work, "init", "--force", "--source", "github.com/x/y"); err != nil {
		t.Fatalf("init --force: %v", err)
	}
	body := readFile(t, configPath)
	if !strings.Contains(body, "github.com/x/y") {
		t.Errorf("--force should overwrite the file; got:\n%s", body)
	}
}
