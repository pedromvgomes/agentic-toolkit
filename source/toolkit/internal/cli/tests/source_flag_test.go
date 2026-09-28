package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The --source workflow: apply a toolkit tree that lives elsewhere on
// disk, as if agtk were run there. Definitions resolve against the
// source's definitions/, but rendered output and the lockfile land in
// the apply dir (cwd), leaving the source tree untouched.
func TestSync_SourceFlag_RendersFromSourceIntoApplyDir(t *testing.T) {
	// A self-contained, all-local toolkit source (bare-name skill, no
	// external URLs) so the whole flow runs offline.
	source := t.TempDir()
	if err := copyTree("testdata/primary", source); err != nil {
		t.Fatalf("copy source tree: %v", err)
	}

	apply := t.TempDir()
	cache := t.TempDir()

	_, stderr, err := runCLI(t, apply, "--source", source, "--stack", "default", "sync", "--cache", cache)
	if err != nil {
		t.Fatalf("sync --source: %v\nstderr:\n%s", err, stderr)
	}

	// Definition resolved from the source's definitions/, rendered into
	// the apply dir's .claude/.
	renderedSkill := filepath.Join(apply, ".claude", "skills", "foo", "SKILL.md")
	if body, err := os.ReadFile(renderedSkill); err != nil {
		t.Errorf("expected rendered skill at %q: %v", renderedSkill, err)
	} else if !strings.Contains(string(body), "Body of the foo skill") {
		t.Errorf("rendered skill missing expected body:\n%s", body)
	}

	// Lockfile lands in the apply dir, never in the shared source tree.
	if _, err := os.Stat(filepath.Join(apply, ".agentic-toolkit.lock.yaml")); err != nil {
		t.Errorf("expected lockfile in apply dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(source, ".agentic-toolkit.lock.yaml")); !os.IsNotExist(err) {
		t.Errorf("source tree must stay clean, but a lockfile exists there (err=%v)", err)
	}
	// And no stray .claude/ written into the source tree.
	if _, err := os.Stat(filepath.Join(source, ".claude")); !os.IsNotExist(err) {
		t.Errorf("source tree must not receive rendered output (err=%v)", err)
	}
}

// --stack renders exactly the named stack. The source tree's own convention
// root belongs to its entry manifest, which --stack does not read, so nothing
// under it reaches the apply dir.
func TestSync_SourceStack_SkipsSourceConventionRoot(t *testing.T) {
	source := t.TempDir()
	if err := copyTree("testdata/primary", source); err != nil {
		t.Fatalf("copy source tree: %v", err)
	}
	scanned := filepath.Join(source, "agentic", "instructions", "house-style.md")
	if err := os.MkdirAll(filepath.Dir(scanned), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "---\ndescription: An instruction scoped to the source repo itself.\n---\n\nOnly the source repo wants this.\n"
	if err := os.WriteFile(scanned, []byte(body), 0o644); err != nil {
		t.Fatalf("write instruction: %v", err)
	}

	apply := t.TempDir()
	cache := t.TempDir()

	_, stderr, err := runCLI(t, apply, "--source", source, "--stack", "default", "sync", "--cache", cache)
	if err != nil {
		t.Fatalf("sync --source --stack: %v\nstderr:\n%s", err, stderr)
	}

	// The named stack's own entry still renders.
	if _, err := os.Stat(filepath.Join(apply, ".claude", "skills", "foo", "SKILL.md")); err != nil {
		t.Errorf("expected the named stack's skill to render: %v", err)
	}
	if found := grepTree(t, apply, "Only the source repo wants this."); found != "" {
		t.Errorf("convention-scanned instruction leaked into %s", found)
	}
}

// grepTree returns the first file under root whose contents hold needle, or
// "" when none does.
func grepTree(t *testing.T, root, needle string) string {
	t.Helper()
	var hit string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || hit != "" {
			return err
		}
		body, rerr := os.ReadFile(p) // #nosec G304 -- walks a test temp dir
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(body), needle) {
			hit = p
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return hit
}

// --source and --config cannot be combined.
func TestSource_ConflictsWithConfig(t *testing.T) {
	dir := t.TempDir()
	_, _, err := runCLI(t, dir, "--source", dir, "--config", filepath.Join(dir, "x.yaml"), "plan")
	if err == nil {
		t.Fatalf("expected error combining --source and --config")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error should explain mutual exclusivity, got: %v", err)
	}
}

// --stack without --source is rejected.
func TestStack_RequiresSource(t *testing.T) {
	dir := t.TempDir()
	_, _, err := runCLI(t, dir, "--stack", "default", "plan")
	if err == nil {
		t.Fatalf("expected error using --stack without --source")
	}
	if !strings.Contains(err.Error(), "requires --source") {
		t.Errorf("error should explain --stack requires --source, got: %v", err)
	}
}
