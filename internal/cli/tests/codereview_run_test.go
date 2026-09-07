package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// reviewRepo is a repository with a change to review and a rules document.
func reviewRepo(t *testing.T) (dir, base string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	write := func(path, body string) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(path))
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
	write("CLAUDE.md", "# House rules\n\nNever narrate the change.\n")
	write("a.go", "package main\n\nfunc main() {}\n")
	run("add", "-A")
	run("commit", "-m", "base")
	base = strings.TrimSpace(run("rev-parse", "HEAD"))

	write("a.go", "package main\n\nfunc main() { panic(\"boom\") }\n")
	run("add", "-A")
	run("commit", "-m", "the change")
	return dir, base
}

// --dry-run assembles everything a review would send and starts no process, so
// it works on a machine with no agent CLI installed at all.
func TestCodeReviewRunDryRunSpendsNothingAndNeedsNoCLI(t *testing.T) {
	dir, base := reviewRepo(t)
	t.Setenv("PATH", gitOnlyPath(t))

	stdout, stderr, err := runCLI(t, dir, "code-review", "run", "--dry-run", "--base", base)
	if err != nil {
		t.Fatalf("--dry-run needs something it should not: %v\n%s", err, stderr)
	}
	for _, want := range []string{
		"panel:", "run(s) would be made", "nothing was spent",
		"=== prompt for", "Never narrate the change", "panic(",
		"security:prompt-injection",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--dry-run omits %q from its output", want)
		}
	}
}

// The review root is a temporary directory, and a preview that left one behind
// would litter the machine on every invocation.
func TestCodeReviewRunDryRunLeavesNoReviewRootBehind(t *testing.T) {
	dir, base := reviewRepo(t)
	t.Setenv("PATH", gitOnlyPath(t))

	stdout, _, err := runCLI(t, dir, "code-review", "run", "--dry-run", "--base", base)
	if err != nil {
		t.Fatal(err)
	}
	var rootPath string
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "root:") {
			rootPath = strings.TrimSpace(strings.TrimPrefix(line, "root:"))
		}
	}
	if rootPath == "" {
		t.Fatalf("--dry-run did not name its review root:\n%s", stdout)
	}
	if _, err := os.Stat(rootPath); !os.IsNotExist(err) {
		t.Errorf("the review root survives the preview at %s", rootPath)
	}
}

func TestCodeReviewRunDryRunJSONCarriesThePlannedRuns(t *testing.T) {
	dir, base := reviewRepo(t)
	t.Setenv("PATH", gitOnlyPath(t))

	stdout, stderr, err := runCLI(t, dir, "code-review", "run", "--dry-run", "--json", "--base", base)
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	var got struct {
		Version int    `json:"version"`
		Panel   string `json:"panel"`
		Root    string `json:"review_root"`
		WorkDir string `json:"review_workdir"`
		Runs    []struct {
			Label  string `json:"label"`
			Role   string `json:"role"`
			Prompt string `json:"prompt"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("--json is not valid JSON: %v\n%s", err, stdout)
	}
	if got.Version != 1 {
		t.Errorf("version is %d", got.Version)
	}
	if got.Panel == "" || len(got.Runs) == 0 {
		t.Fatalf("the plan is empty: %+v", got)
	}
	if got.Root == got.WorkDir {
		t.Error("the review root and the workdir are the same directory")
	}
	if !strings.Contains(got.Runs[0].Prompt, "Never narrate the change") {
		t.Error("the JSON plan's prompt does not carry the repo's rules")
	}
}

// A review needs an agent CLI, so on a machine with none it must refuse by
// saying so rather than reporting a clean review.
func TestCodeReviewRunWithNoCLIInstalledRefusesRatherThanReportingClean(t *testing.T) {
	dir, base := reviewRepo(t)
	t.Setenv("PATH", gitOnlyPath(t))

	stdout, _, err := runCLI(t, dir, "code-review", "run", "--base", base)
	if err == nil {
		t.Fatalf("a review ran with no CLI installed:\n%s", stdout)
	}
	if strings.Contains(stdout, "No findings survived") {
		t.Errorf("a review that could not run reported itself as clean:\n%s", stdout)
	}
}

func TestCodeReviewRunRefusesAnUnknownContext(t *testing.T) {
	dir, base := reviewRepo(t)
	t.Setenv("PATH", gitOnlyPath(t))

	_, _, err := runCLI(t, dir, "code-review", "run", "--dry-run", "--base", base, "--context", "nope")
	if err == nil {
		t.Fatal("an unknown context was accepted")
	}
	if !strings.Contains(err.Error(), "worktree") {
		t.Errorf("the refusal does not list the contexts: %v", err)
	}
}

func TestCodeReviewRunRefusesAnUnknownPanel(t *testing.T) {
	dir, base := reviewRepo(t)
	t.Setenv("PATH", gitOnlyPath(t))

	if _, _, err := runCLI(t, dir, "code-review", "run", "--dry-run", "--base", base, "--panel", "nope"); err == nil {
		t.Fatal("an unknown panel was accepted")
	}
}

// The bare group prints help rather than taking a subcommand name as an
// argument and exiting 0.
func TestCodeReviewRunIsListedOnTheCommandGroup(t *testing.T) {
	stdout, _, err := runCLI(t, t.TempDir(), "code-review", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"run", "explain", "signals"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the command group does not list %q:\n%s", want, stdout)
		}
	}
}

// `run` and `explain` describe the same change the same way. A run that
// rendered the merge-base commit id would hand back a bare hash where the
// caller wrote a branch name.
func TestCodeReviewRunAndExplainNameTheRangeIdentically(t *testing.T) {
	dir, _ := reviewRepo(t)
	t.Setenv("PATH", gitOnlyPath(t))

	rangeOf := func(out string) string {
		t.Helper()
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "range:") {
				return strings.TrimSpace(strings.TrimPrefix(line, "range:"))
			}
		}
		t.Fatalf("no range line in:\n%s", out)
		return ""
	}

	explainOut, _, err := runCLI(t, dir, "code-review", "explain", "--base", "main")
	if err != nil {
		t.Fatal(err)
	}
	runOut, _, err := runCLI(t, dir, "code-review", "run", "--dry-run", "--base", "main")
	if err != nil {
		t.Fatal(err)
	}

	want, got := rangeOf(explainOut), rangeOf(runOut)
	if got != want {
		t.Errorf("run reports the range as %q and explain as %q", got, want)
	}
	if !strings.Contains(got, "main") {
		t.Errorf("the range does not name the ref the caller gave: %q", got)
	}
}
