package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitProject builds a throwaway repository with one commit on main and the
// named files left uncommitted, which is the working-tree change a review
// would look at.
func gitProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "-b", "main")
	run("config", "user.email", "test@example.invalid")
	run("config", "user.name", "Test")
	write("README.md", "# base\n")
	run("add", "-A")
	run("commit", "-m", "base")

	for name, body := range files {
		write(name, body)
	}
	return dir
}

// Deciding which panel a change gets must not need a model, a provider or an
// agent CLI. It is the answer to "why is this review deeper than I expected",
// and it has to be available before paying for the review that would tell you.
func TestCodeReviewExplainNeedsNoProvider(t *testing.T) {
	work := gitProject(t, map[string]string{
		"internal/auth/token.go": "package auth\n\nfunc Verify() bool { return true }\n",
	})

	stdout, stderr, err := runCLI(t, work, "code-review", "explain", "--base", "main")
	if err != nil {
		t.Fatalf("explain: %v\n%s", err, stderr)
	}

	for _, want := range []string{
		"manifest: built-in default",
		"context: worktree",
		"default: quick",
		"panel:",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("explain output missing %q:\n%s", want, stdout)
		}
	}
}

// A repo that believes it wrote a manifest and is being reviewed by the
// built-in one needs to be told: the two produce entirely different panels,
// and nothing else in the output would say which was read.
func TestCodeReviewExplainNamesWhichManifestItRead(t *testing.T) {
	manifest := `version: 1
reviewers:
  correctness: {provider: claudecode, prompt: builtin:correctness}
judge:     {provider: claudecode, prompt: builtin:judge}
validator: {provider: claudecode, prompt: builtin:validator}
panels:
  only: {reviewers: [correctness]}
defaults: {worktree: only, pr: only}
`
	work := gitProject(t, map[string]string{
		".agents/code-review/manifest.yaml": manifest,
		"main.go":                           "package main\n",
	})

	stdout, stderr, err := runCLI(t, work, "code-review", "explain", "--base", "main")
	if err != nil {
		t.Fatalf("explain: %v\n%s", err, stderr)
	}
	if strings.Contains(stdout, "built-in default") {
		t.Errorf("explain read the built-in default over the repo's own manifest:\n%s", stdout)
	}
	if !strings.Contains(stdout, "panel:   only") {
		t.Errorf("explain did not run the repo's own panel:\n%s", stdout)
	}
}

// A manifest that cannot staff a review is refused before anything is spent,
// and the refusal names the field.
func TestCodeReviewExplainRefusesABrokenManifest(t *testing.T) {
	work := gitProject(t, map[string]string{
		".agents/code-review/manifest.yaml": `version: 1
reviewers:
  correctness: {provider: claudecode, prompt: builtin:correctness}
judge:     {provider: claudecode, prompt: builtin:judge}
validator: {provider: claudecode, prompt: builtin:validator}
panels:
  only: {reviewers: [typo]}
defaults: {worktree: only, pr: only}
`,
	})

	_, _, err := runCLI(t, work, "code-review", "explain", "--base", "main")
	if err == nil {
		t.Fatal("explain accepted a panel naming a reviewer that does not exist")
	}
	if !strings.Contains(err.Error(), "panels.only.reviewers[0]") {
		t.Errorf("refusal does not name the field: %v", err)
	}
}

// The vocabulary is closed and ships with the binary, so listing it needs
// neither a repository nor a manifest.
func TestCodeReviewSignalsListsTheVocabulary(t *testing.T) {
	stdout, stderr, err := runCLI(t, t.TempDir(), "code-review", "signals")
	if err != nil {
		t.Fatalf("signals: %v\n%s", err, stderr)
	}
	for _, want := range []string{"auth", "concurrency", "fix-revert", "shared-kernel"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("signals output missing %q:\n%s", want, stdout)
		}
	}
	// Each name carries the one-line account of what it says about a change,
	// so the list answers "which of these do I want" rather than only "what
	// may I write".
	if !strings.Contains(stdout, "middleware that gates requests") {
		t.Errorf("signals output has names without descriptions:\n%s", stdout)
	}
}

// Cobra rejects an unknown subcommand only at the root: a non-root parent
// takes it as an argument, prints help and exits 0. An agent told to run a
// subcommand this binary does not have would read that help as the command's
// output and report success.
func TestAnUnknownCodeReviewSubcommandFails(t *testing.T) {
	if _, _, err := runCLI(t, t.TempDir(), "code-review", "expalin"); err == nil {
		t.Error("an unknown subcommand printed help and exited 0")
	}
}
