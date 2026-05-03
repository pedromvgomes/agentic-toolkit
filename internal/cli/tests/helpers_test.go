package tests

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/cli"
)

// setupGitEnv isolates git from the host's gitconfig so tests are
// hermetic against the developer's hooks, signing config, and aliases.
func setupGitEnv(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
}

// fixtureRepoFromDir creates a bare git repo at <tmp>/repo.git seeded
// from the contents of srcDir, and returns its file:// URL plus the
// HEAD commit SHA. The bare path ends in `.git` so direct-ref URLs
// (URL + "/skills/foo") exercise the sourcestore URL splitter.
func fixtureRepoFromDir(t *testing.T, srcDir string) (url, sha string) {
	t.Helper()
	setupGitEnv(t)
	base := t.TempDir()
	work := filepath.Join(base, "work")
	bare := filepath.Join(base, "repo.git")

	if err := copyTree(srcDir, work); err != nil {
		t.Fatalf("copy testdata: %v", err)
	}
	runGitOK(t, work, "init", "--quiet", "-b", "main")
	runGitOK(t, work,
		"-c", "user.email=t@t",
		"-c", "user.name=t",
		"add", ".",
	)
	runGitOK(t, work,
		"-c", "user.email=t@t",
		"-c", "user.name=t",
		"-c", "commit.gpgsign=false",
		"commit", "--quiet", "-m", "initial",
	)
	runGitOK(t, work, "clone", "--bare", "--quiet", ".", bare)
	out := runGitOut(t, work, "rev-parse", "HEAD")
	return "file://" + bare, strings.TrimSpace(out)
}

// runCLI builds a fresh root command, points it at workDir, captures
// stdout/stderr, and runs args. The captured streams plus the returned
// error let tests assert on observable behavior without touching os.*.
func runCLI(t *testing.T, workDir string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	env := &cli.Env{
		Stdin:   strings.NewReader(""),
		Stdout:  &outBuf,
		Stderr:  &errBuf,
		WorkDir: workDir,
	}
	root := cli.NewRootCmd(env)
	root.SetArgs(args)
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

func runGitOK(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, string(out))
	}
}

func runGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(out)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeEntryStack writes a minimal entry-point stack manifest at
// <workDir>/.agentic-toolkit.yaml that extends the test fixture's
// `stacks/default.yaml`. sourceURL is the bare-repo URL returned by
// fixtureRepoFromDir; ref is typically "main". Used everywhere the old
// `source: <url>@main\npresets:\n  - default\n` body was written.
func writeEntryStack(t *testing.T, workDir, sourceURL, ref string) {
	t.Helper()
	body := "extends:\n  - " + sourceURL + "/stacks/default.yaml@" + ref + "\n"
	writeFile(t, filepath.Join(workDir, ".agentic-toolkit.yaml"), body)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
