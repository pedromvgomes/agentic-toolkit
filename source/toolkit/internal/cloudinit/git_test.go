package cloudinit

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// isolateGit keeps the machine's own git config out of a test.
func isolateGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required by these tests: %v", err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func TestAFailedGitRunReturnsNoOutputAndGitsOwnMessage(t *testing.T) {
	isolateGit(t)
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)

	out, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if out != "" {
		t.Errorf("output = %q, want none from a failed run", out)
	}
	if err == nil || !strings.HasPrefix(err.Error(), "cloud init: git rev-parse --show-toplevel: exit status 128: ") ||
		!strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("err = %v, want git's own message after the exit status", err)
	}
}

func TestASilentGitFailureCarriesOnlyTheExitStatus(t *testing.T) {
	isolateGit(t)
	ctx := context.Background()
	repo := t.TempDir()
	if _, err := git(ctx, repo, "init", "-q"); err != nil {
		t.Fatal(err)
	}

	out, err := git(ctx, repo, "config", "--local", "--get", "no.such")
	if out != "" {
		t.Errorf("output = %q, want none from a failed run", out)
	}
	const want = "cloud init: git config --local --get no.such: exit status 1"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}
