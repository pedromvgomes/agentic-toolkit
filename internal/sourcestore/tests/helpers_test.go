package tests

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupGitEnv isolates git from the host's global/system configuration
// so tests are hermetic against the developer's gitconfig, hooks, and
// commit signing setup.
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

// fixtureRepo seeds a working directory with `files` (path -> content),
// commits them, and clones the result to a bare repository at
// <tmp>/repo.git. Returns the bare-repo's file:// URL and the HEAD SHA
// of the initial commit.
//
// The bare path ends in `.git`, which lets test URLs exercise the
// `.git/` URL splitter for direct-ref refs:
//
//	url + "/skills/foo"  → repo URL + in-repo path "skills/foo"
func fixtureRepo(t *testing.T, files map[string]string) (url, sha string) {
	t.Helper()
	setupGitEnv(t)
	base := t.TempDir()
	work := filepath.Join(base, "work")
	bare := filepath.Join(base, "repo.git")

	runGitOK(t, "", "init", "--quiet", "-b", "main", work)
	for relPath, content := range files {
		full := filepath.Join(work, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
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

// commitSecond appends a second commit to the bare repo identified by
// barePath (the on-disk path, not the file:// URL) carrying the given
// files. Returns the new HEAD SHA. Used to verify that lockfile pins
// remain stable across remote history evolution.
func commitSecond(t *testing.T, barePath string, files map[string]string) (sha string) {
	t.Helper()
	work := t.TempDir()
	runGitOK(t, "", "clone", "--quiet", barePath, work)
	for relPath, content := range files {
		full := filepath.Join(work, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	runGitOK(t, work,
		"-c", "user.email=t@t",
		"-c", "user.name=t",
		"add", ".",
	)
	runGitOK(t, work,
		"-c", "user.email=t@t",
		"-c", "user.name=t",
		"-c", "commit.gpgsign=false",
		"commit", "--quiet", "-m", "second",
	)
	runGitOK(t, work, "push", "--quiet", "origin", "main")
	return strings.TrimSpace(runGitOut(t, work, "rev-parse", "HEAD"))
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

// readFSFile reads a file from an fs.FS for assertion convenience.
func readFSFile(t *testing.T, fsys fs.FS, name string) string {
	t.Helper()
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}
