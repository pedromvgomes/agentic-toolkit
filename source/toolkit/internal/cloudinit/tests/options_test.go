package tests

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/cloudinit"
)

// With every Options field zero, Run reads the process environment, installs
// under $HOME, fills in the checkout at the working directory and bounds
// ssh-keygen by DefaultTimeout rather than by a zero that expires at once.
func TestZeroOptionsUseTheProcessEnvironmentHomeAndWorkingDirectory(t *testing.T) {
	home := fakeHome(t)
	repo := newRepo(t)
	t.Chdir(repo)
	key := newKey(t, "")
	t.Setenv(cloudinit.EnvUser, "Ada Lovelace")
	t.Setenv(cloudinit.EnvEmail, "")
	t.Setenv(cloudinit.EnvSigningKey, key)

	if err := cloudinit.Run(context.Background(), cloudinit.Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertInstalled(t, home, key)
	if got, want := globalConfig(t, "user.signingkey"), filepath.Join(home, ".ssh", cloudinit.KeyName)+".pub"; got != want {
		t.Errorf("user.signingkey = %q, want %q", got, want)
	}
	if got := gitIn(t, repo, "config", "--local", "user.name"); got != "Ada Lovelace" {
		t.Errorf("local user.name = %q, want the working directory's checkout filled in", got)
	}
}

func TestANegativeTimeoutFallsBackToTheDefault(t *testing.T) {
	home := fakeHome(t)
	key := newKey(t, "")

	if _, err := runWith(t, cloudinit.Options{
		Getenv:  env(map[string]string{cloudinit.EnvSigningKey: key}),
		Home:    home,
		Dir:     t.TempDir(),
		Timeout: -1,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertInstalled(t, home, key)
}

// Options.Home moves the key and leaves git's global config where $HOME puts
// it.
func TestTheKeyGoesUnderOptionsHomeRatherThanHOME(t *testing.T) {
	home := fakeHome(t)
	elsewhere := t.TempDir()
	key := newKey(t, "")

	if _, err := run(t, elsewhere, t.TempDir(), map[string]string{cloudinit.EnvSigningKey: key}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertInstalled(t, elsewhere, key)
	if got, want := globalConfig(t, "user.signingkey"), filepath.Join(elsewhere, ".ssh", cloudinit.KeyName)+".pub"; got != want {
		t.Errorf("user.signingkey = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh")); !os.IsNotExist(err) {
		t.Errorf("$HOME/.ssh was touched with Options.Home set elsewhere: %v", err)
	}
}

func TestAnUnresolvableHomeFailsBeforeWritingAnything(t *testing.T) {
	fakeHome(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	t.Setenv("HOME", "")

	_, err := run(t, "", cwd, map[string]string{cloudinit.EnvSigningKey: newKey(t, "")})
	if err == nil || !strings.Contains(err.Error(), "cloud init: resolve the home directory: ") {
		t.Fatalf("Run = %v, want a failure to resolve the home directory", err)
	}
	if entries, _ := os.ReadDir(cwd); len(entries) != 0 {
		t.Errorf("the run wrote into the working directory: %v", entries)
	}
}

func TestAnUnreadableWorkingDirectoryFails(t *testing.T) {
	home := fakeHome(t)
	gone := t.TempDir()
	t.Chdir(gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}

	_, err := run(t, home, "", map[string]string{cloudinit.EnvUser: "Ada Lovelace"})
	if err == nil || !strings.Contains(err.Error(), "cloud init: resolve the working directory: ") {
		t.Fatalf("Run = %v, want a failure to resolve the working directory", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".gitconfig")); !os.IsNotExist(err) {
		t.Errorf("the global config was written: %v", err)
	}
}
