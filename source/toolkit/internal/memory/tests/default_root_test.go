package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

// TestDefaultRootIsOutsideEveryRenderedTree: a store is committed content,
// and a rendered root is ignored wholesale by the repos that render into it,
// so a default pointing inside one produces notes that are never committed
// and says nothing about it.
func TestDefaultRootIsOutsideEveryRenderedTree(t *testing.T) {
	for _, rendered := range []string{".claude", ".codex", ".agents"} {
		if memory.DefaultRoot == rendered || strings.HasPrefix(memory.DefaultRoot, rendered+"/") {
			t.Errorf("DefaultRoot %q sits inside the rendered tree %q", memory.DefaultRoot, rendered)
		}
	}
	// A memory store is what agents wrote, not how agtk behaves, so it also
	// stays out of the toolkit namespace.
	if strings.HasPrefix(memory.DefaultRoot, ".agentic-toolkit") {
		t.Errorf("DefaultRoot %q claims the toolkit namespace, which holds configuration", memory.DefaultRoot)
	}
}

// TestLegacyRootIsReportedNotReadAsUnadopted: an absent store otherwise means
// the repo never adopted memory, which would index an empty store over a full
// one left at the old default.
func TestLegacyRootIsReportedNotReadAsUnadopted(t *testing.T) {
	project := t.TempDir()
	mkdirAll(t, filepath.Join(project, filepath.FromSlash(memory.LegacyDefaultRoot), "notes"))

	err := memory.New(project, "").CheckLegacyRoot()
	if err == nil {
		t.Fatal("a store at the old default was read as a repo that never adopted memory")
	}
	for _, want := range []string{memory.LegacyDefaultRoot, memory.DefaultRoot, "git mv", "memory.root"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not name %q: %v", want, err)
		}
	}
}

// TestLegacyRootIsSilentOnceMigrated: a repo that moved its store keeps a
// working `agtk memory` even with the old directory still lying around.
func TestLegacyRootIsSilentOnceMigrated(t *testing.T) {
	project := t.TempDir()
	mkdirAll(t, filepath.Join(project, filepath.FromSlash(memory.LegacyDefaultRoot), "notes"))
	mkdirAll(t, filepath.Join(project, memory.DefaultRoot, "notes"))

	if err := memory.New(project, "").CheckLegacyRoot(); err != nil {
		t.Errorf("refused a repo whose store is already at the default: %v", err)
	}
}

// TestConfiguredRootIsNeverSecondGuessed: the rule that moved this default
// binds the path agtk fixes, not the one a consumer picks for its own repo.
func TestConfiguredRootIsNeverSecondGuessed(t *testing.T) {
	project := t.TempDir()
	mkdirAll(t, filepath.Join(project, filepath.FromSlash(memory.LegacyDefaultRoot), "notes"))

	if err := memory.New(project, memory.LegacyDefaultRoot).CheckLegacyRoot(); err != nil {
		t.Errorf("refused an explicitly configured memory.root: %v", err)
	}
}

// TestNoStoreAnywhereIsStillUnadopted: the refusal fires on a store that
// exists, never on a repo with no notes at all.
func TestNoStoreAnywhereIsStillUnadopted(t *testing.T) {
	if err := memory.New(t.TempDir(), "").CheckLegacyRoot(); err != nil {
		t.Errorf("refused a repo that has not adopted memory: %v", err)
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
