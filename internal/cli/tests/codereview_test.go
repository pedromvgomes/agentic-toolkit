package tests

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGit runs one git command in dir, failing the test if it does not succeed.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// writeIn writes one file at a repo-relative path inside dir.
func writeIn(t *testing.T, dir, name, body string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, filepath.FromSlash(name)), body)
}

// gitProject builds a throwaway repository with one commit on main and the
// named files left uncommitted, which is the working-tree change a review
// would look at.
func gitProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) { runGit(t, dir, args...) }
	write := func(name, body string) { writeIn(t, dir, name, body) }

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

// Typing the command with no flags is the common case, so the ref a change is
// measured against when nobody names one has to be exercised.
func TestCodeReviewExplainDetectsTheBaseWhenNoneIsNamed(t *testing.T) {
	work := gitProject(t, map[string]string{"added.go": "package main\n"})

	stdout, stderr, err := runCLI(t, work, "code-review", "explain")
	if err != nil {
		t.Fatalf("explain: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "range:    main...working tree") {
		t.Errorf("explain did not detect main as the base:\n%s", stdout)
	}
}

// A repo matching none of the conventions gets a refusal naming what was
// tried, rather than a change measured against a base nobody chose.
func TestCodeReviewExplainRefusesWhenNoBaseCanBeDetected(t *testing.T) {
	work := gitProject(t, map[string]string{"added.go": "package main\n"})
	runGit(t, work, "branch", "-m", "main", "trunk")

	_, _, err := runCLI(t, work, "code-review", "explain")
	if err == nil {
		t.Fatal("explain measured the change against a base it never found")
	}
	for _, want := range []string{"no base branch found", "origin/HEAD", "--base"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %q, want it to mention %q", err, want)
		}
	}
}

// A review that posts reads its rules from the base ref. Everything on the
// branch under review is written by its author, so a manifest read from the
// working tree would let a change name the reviewers that judge it.
func TestAPostingContextReadsTheManifestFromTheBaseRef(t *testing.T) {
	onBase := `version: 1
reviewers:
  correctness: {provider: claudecode, prompt: builtin:correctness}
judge:     {provider: claudecode, prompt: builtin:judge}
validator: {provider: claudecode, prompt: builtin:validator}
panels:
  trusted: {reviewers: [correctness]}
defaults: {worktree: trusted, pr: trusted}
`
	work := gitProject(t, nil)
	writeIn(t, work, ".agents/code-review/manifest.yaml", onBase)
	runGit(t, work, "add", "-A")
	runGit(t, work, "commit", "-m", "declare the review")

	// The branch rewrites the rules it will be judged by.
	writeIn(t, work, ".agents/code-review/manifest.yaml",
		strings.ReplaceAll(onBase, "trusted", "rewritten"))

	pr, _, err := runCLI(t, work, "code-review", "explain", "--context", "pr", "--base", "HEAD")
	if err != nil {
		t.Fatalf("explain --context pr: %v", err)
	}
	if strings.Contains(pr, "rewritten") {
		t.Errorf("a posting review read the manifest from the branch under review:\n%s", pr)
	}
	if !strings.Contains(pr, "panel:   trusted") {
		t.Errorf("a posting review did not use the base ref's panel:\n%s", pr)
	}

	// A local review of the working tree is the author reviewing their own
	// change, so it reads what they have written.
	worktree, _, err := runCLI(t, work, "code-review", "explain", "--base", "HEAD")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if !strings.Contains(worktree, "panel:   rewritten") {
		t.Errorf("a worktree review should read the working tree:\n%s", worktree)
	}
}
