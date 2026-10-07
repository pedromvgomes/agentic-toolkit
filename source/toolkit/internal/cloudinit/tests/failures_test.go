package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/cloudinit"
)

// signingUntouched fails the test if any signing key in the global config
// moved off the value the test seeded.
func signingUntouched(t *testing.T) {
	t.Helper()
	if got := globalConfig(t, "gpg.ssh.program"); got != "/tmp/code-sign" {
		t.Errorf("gpg.ssh.program = %q, want it untouched by a run that failed", got)
	}
	for _, k := range []string{"gpg.format", "user.signingkey", "commit.gpgsign", "tag.gpgsign"} {
		if got := globalConfig(t, k); got != "" {
			t.Errorf("%s = %q, want it unset after a run that failed", k, got)
		}
	}
}

// seedSigning gives the fake home a gpg.ssh.program a failed run must leave
// alone.
func seedSigning(t *testing.T, home string) {
	t.Helper()
	gitIn(t, home, "config", "--global", "gpg.ssh.program", "/tmp/code-sign")
}

// A key ssh-keygen loads but cannot sign with never becomes the configured
// one.
func TestAKeyThatCannotSignIsNeverConfigured(t *testing.T) {
	home := fakeHome(t)
	seedSigning(t, home)
	shadowTool(t, "ssh-keygen", `if [ "$1" = "-Y" ]; then exit 1; fi
exec "$REAL" "$@"`)

	_, err := run(t, home, t.TempDir(), map[string]string{cloudinit.EnvSigningKey: newKey(t, "")})
	const want = "cloud init: ssh-keygen -Y sign rejected the key from " + cloudinit.EnvSigningKey + ": exit status 1"
	if err == nil || err.Error() != want {
		t.Fatalf("Run = %v, want %q", err, want)
	}
	signingUntouched(t)
}

func TestAKeyRejectedOnLoadIsNamedWithSSHKeygensOwnMessage(t *testing.T) {
	home := fakeHome(t)
	seedSigning(t, home)

	_, err := run(t, home, t.TempDir(), map[string]string{
		cloudinit.EnvSigningKey: newKey(t, "")[:120],
	})
	const prefix = "cloud init: ssh-keygen -y rejected the key from " + cloudinit.EnvSigningKey + ": exit status "
	if err == nil || !strings.HasPrefix(err.Error(), prefix) {
		t.Fatalf("Run = %v, want an error starting %q", err, prefix)
	}
	if !strings.Contains(err.Error(), "Load key") {
		t.Errorf("the error drops ssh-keygen's own message: %q", err)
	}
	signingUntouched(t)
}

func TestAFailedFingerprintIsReportedWithSSHKeygensMessage(t *testing.T) {
	home := fakeHome(t)
	shadowTool(t, "ssh-keygen", `if [ "$1" = "-l" ]; then echo "cannot fingerprint" >&2; exit 1; fi
exec "$REAL" "$@"`)

	out, err := run(t, home, t.TempDir(), map[string]string{cloudinit.EnvSigningKey: newKey(t, "")})
	const want = "cloud init: ssh-keygen -l rejected the key from " + cloudinit.EnvSigningKey + ": exit status 1: cannot fingerprint"
	if err == nil || err.Error() != want {
		t.Fatalf("Run = %v, want %q", err, want)
	}
	if strings.Contains(out, "signed with") {
		t.Errorf("stdout reports signing that the run could not confirm: %q", out)
	}
}

func TestAnSSHKeygenThatHangsIsStoppedByTheTimeout(t *testing.T) {
	home := fakeHome(t)
	seedSigning(t, home)
	shadowTool(t, "ssh-keygen", `if [ "$1" = "-y" ]; then exec sleep 30; fi
exec "$REAL" "$@"`)

	start := time.Now()
	_, err := runWith(t, cloudinit.Options{
		Getenv:  env(map[string]string{cloudinit.EnvSigningKey: newKey(t, "")}),
		Home:    home,
		Dir:     t.TempDir(),
		Timeout: 300 * time.Millisecond,
	})
	const want = "cloud init: ssh-keygen -y did not finish within 300ms; " + cloudinit.EnvSigningKey + " must hold a key that needs no passphrase"
	if err == nil || err.Error() != want {
		t.Fatalf("Run = %v, want %q", err, want)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Errorf("Run took %s, want it stopped near the timeout", elapsed)
	}
	signingUntouched(t)
	assertNoTempFiles(t, home)
}

func TestAMissingSSHKeygenFailsNamingIt(t *testing.T) {
	home := fakeHome(t)
	seedSigning(t, home)
	key := newKey(t, "")
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	gitPath := filepath.Join(t.TempDir(), "git")
	if err := os.Symlink(real, gitPath); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(gitPath))

	_, err = run(t, home, t.TempDir(), map[string]string{cloudinit.EnvSigningKey: key})
	if err == nil || !strings.HasPrefix(err.Error(), `cloud init: ssh-keygen -y: exec: "ssh-keygen": `) {
		t.Fatalf("Run = %v, want a failure naming the missing ssh-keygen", err)
	}
	signingUntouched(t)
	assertNoTempFiles(t, home)
}

func TestAnSSHPathThatIsAFileFails(t *testing.T) {
	home := fakeHome(t)
	seedSigning(t, home)
	if err := os.WriteFile(filepath.Join(home, ".ssh"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := run(t, home, t.TempDir(), map[string]string{cloudinit.EnvSigningKey: newKey(t, "")})
	if err == nil || !strings.HasPrefix(err.Error(), "cloud init: create "+filepath.Join(home, ".ssh")+": ") {
		t.Fatalf("Run = %v, want a failure to create ~/.ssh", err)
	}
	signingUntouched(t)
}

func TestAKeyPathHeldByADirectoryFailsAndLeavesNoTempFile(t *testing.T) {
	home := fakeHome(t)
	seedSigning(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".ssh", cloudinit.KeyName), 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := run(t, home, t.TempDir(), map[string]string{cloudinit.EnvSigningKey: newKey(t, "")})
	if err == nil || !strings.HasPrefix(err.Error(), "cloud init: move "+cloudinit.KeyName+" into place: ") {
		t.Fatalf("Run = %v, want a failure to move the key into place", err)
	}
	signingUntouched(t)
	assertNoTempFiles(t, home)
}

func TestASigningCheckWithNowhereToWriteFails(t *testing.T) {
	home := fakeHome(t)
	seedSigning(t, home)
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))

	_, err := run(t, home, t.TempDir(), map[string]string{cloudinit.EnvSigningKey: newKey(t, "")})
	if err == nil || !strings.HasPrefix(err.Error(), "cloud init: create a directory for the signing check: ") {
		t.Fatalf("Run = %v, want a failure to create the signing check's directory", err)
	}
	signingUntouched(t)
}

// An unwritable global config fails the run with git's own message, whether
// the write is an identity key or a signing key.
func TestAnUnwritableGlobalConfigFailsWithGitsMessage(t *testing.T) {
	for name, vars := range map[string]func(t *testing.T) map[string]string{
		"identity": func(*testing.T) map[string]string { return map[string]string{cloudinit.EnvUser: "Ada Lovelace"} },
		"signing": func(t *testing.T) map[string]string {
			return map[string]string{cloudinit.EnvSigningKey: newKey(t, "")}
		},
	} {
		t.Run(name, func(t *testing.T) {
			home := fakeHome(t)
			vars := vars(t)
			t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "missing", "gitconfig"))

			_, err := run(t, home, t.TempDir(), vars)
			if err == nil {
				t.Fatal("Run succeeded with an unwritable global config")
			}
			if msg := err.Error(); !strings.HasPrefix(msg, "cloud init: git config --global ") || !strings.Contains(msg, "could not lock config file") {
				t.Errorf("Run = %q, want git's own failure to lock the config", msg)
			}
		})
	}
}

func TestACheckoutWhoseIdentityCannotBeReadFails(t *testing.T) {
	home := fakeHome(t)
	repo := newRepo(t)
	shadowTool(t, "git", `if [ "$1 $2 $3" = "config --local --get" ]; then echo "fatal: bad config line 1" >&2; exit 3; fi
exec "$REAL" "$@"`)

	_, err := run(t, home, repo, map[string]string{cloudinit.EnvUser: "Ada Lovelace"})
	const want = "cloud init: git config --local --get user.name: exit status 3: fatal: bad config line 1"
	if err == nil || err.Error() != want {
		t.Fatalf("Run = %v, want %q", err, want)
	}
}

func TestACheckoutWhoseConfigCannotBeWrittenFails(t *testing.T) {
	home := fakeHome(t)
	repo := newRepo(t)
	shadowTool(t, "git", `if [ "$1 $2 $3" = "config --local user.name" ]; then echo "error: could not lock config file" >&2; exit 4; fi
exec "$REAL" "$@"`)

	_, err := run(t, home, repo, map[string]string{cloudinit.EnvUser: "Ada Lovelace"})
	const want = "cloud init: git config --local user.name Ada Lovelace: exit status 4: error: could not lock config file"
	if err == nil || err.Error() != want {
		t.Fatalf("Run = %v, want %q", err, want)
	}
}

func TestACheckoutWhoseTopLevelCannotBeResolvedFails(t *testing.T) {
	home := fakeHome(t)
	repo := newRepo(t)
	shadowTool(t, "git", `if [ "$1 $2" = "rev-parse --show-toplevel" ]; then echo "fatal: no top level" >&2; exit 128; fi
exec "$REAL" "$@"`)

	_, err := run(t, home, repo, map[string]string{cloudinit.EnvUser: "Ada Lovelace"})
	const want = "cloud init: git rev-parse --show-toplevel: exit status 128: fatal: no top level"
	if err == nil || err.Error() != want {
		t.Fatalf("Run = %v, want %q", err, want)
	}
	if got := globalConfig(t, "user.name"); got != "" {
		t.Errorf("global user.name = %q, want nothing written", got)
	}
}
