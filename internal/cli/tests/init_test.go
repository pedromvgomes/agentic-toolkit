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
	if !strings.Contains(body, "extends:") {
		t.Errorf("scaffold should seed an extends list; got:\n%s", body)
	}
}

func TestInit_WithExtendsFlag_SeedsURL(t *testing.T) {
	work := t.TempDir()
	url := "github.com/owner/repo.git/stacks/default.yaml@main"
	_, _, err := runCLI(t, work, "init", "--extends", url)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	body := readFile(t, filepath.Join(work, ".agentic-toolkit.yaml"))
	if !strings.Contains(body, url) {
		t.Errorf("--extends should seed the extends entry; got:\n%s", body)
	}
	if strings.Contains(body, "TODO") {
		t.Errorf("explicit --extends should suppress TODO placeholder; got:\n%s", body)
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

	url := "github.com/x/y.git/stacks/default.yaml@main"
	if _, _, err := runCLI(t, work, "init", "--force", "--extends", url); err != nil {
		t.Fatalf("init --force: %v", err)
	}
	body := readFile(t, configPath)
	if !strings.Contains(body, url) {
		t.Errorf("--force should overwrite the file; got:\n%s", body)
	}
}
