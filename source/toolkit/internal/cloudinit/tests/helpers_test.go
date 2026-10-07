package tests

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/cloudinit"
)

// fakeHome points HOME, and with it git's global config, at a fresh
// directory, and keeps every other config source out, so neither the
// developer's own identity nor their signing setup can leak into a test.
func fakeHome(t *testing.T) string {
	t.Helper()
	requireTool(t, "git")
	requireTool(t, "ssh-keygen")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, name := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_DIR", "GIT_WORK_TREE", "SSH_AUTH_SOCK"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// requireTool fails rather than skips: a suite that skips when the tool is
// missing passes having checked nothing.
func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("%s is required by these tests: %v", name, err)
	}
}

// env builds a Getenv from a map, so a test states exactly which variables
// the run sees.
func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

// run invokes cloudinit.Run against home with the given variables, from dir,
// and returns its stdout. It fails the test if the run does not return within
// a bound well above the run's own timeouts.
func run(t *testing.T, home, dir string, vars map[string]string) (string, error) {
	t.Helper()
	return runWith(t, cloudinit.Options{
		Getenv:  env(vars),
		Home:    home,
		Dir:     dir,
		Timeout: 5 * time.Second,
	})
}

// runWith invokes cloudinit.Run with opts as given, apart from Stdout, and
// returns its stdout under the same bound as run.
func runWith(t *testing.T, opts cloudinit.Options) (string, error) {
	t.Helper()
	var out bytes.Buffer
	opts.Stdout = &out
	done := make(chan error, 1)
	go func() {
		done <- cloudinit.Run(context.Background(), opts)
	}()
	select {
	case err := <-done:
		return out.String(), err
	case <-time.After(60 * time.Second):
		t.Fatal("cloudinit.Run did not return")
		return "", nil
	}
}

// shadowTool puts a shell script named name ahead of the real tool on PATH.
// The script finds the real binary in $REAL, so it can fail one invocation
// and hand every other one on with `exec "$REAL" "$@"`.
func shadowTool(t *testing.T, name, script string) {
	t.Helper()
	requireTool(t, "sh")
	real, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("%s is required by these tests: %v", name, err)
	}
	dir := t.TempDir()
	body := "#!/bin/sh\nREAL='" + real + "'\n" + script + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// newKey generates an ed25519 key with the given passphrase and returns the
// private key file's base64 encoding.
func newKey(t *testing.T, passphrase string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key")
	cmd := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", passphrase, "-C", "test@example.com", "-f", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(data)
}

// newRepo initialises a git repository and returns its path.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	return dir
}

// gitIn runs git in dir and returns its trimmed stdout, failing on error.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if exit, ok := err.(*exec.ExitError); ok {
			stderr = string(exit.Stderr)
		}
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, stderr)
	}
	return strings.TrimSpace(string(out))
}

// globalConfig returns one key from the fake home's global config, or "" when
// it is unset.
func globalConfig(t *testing.T, key string) string {
	t.Helper()
	out, err := exec.Command("git", "config", "--global", "--get", key).Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return ""
		}
		t.Fatalf("git config --global --get %s: %v", key, err)
	}
	return strings.TrimSpace(string(out))
}

// snapshot reads every file under root into a map of relative path to mode
// and contents, so two snapshots compare byte for byte.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if os.IsNotExist(err) && path == root {
			return filepath.SkipDir
		}
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if info.IsDir() {
			files[rel+"/"] = info.Mode().String()
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = info.Mode().String() + "\n" + string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
