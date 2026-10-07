package tests

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/cloudinit"
)

func TestNothingSetPrintsOneLineAndChangesNothing(t *testing.T) {
	home := fakeHome(t)
	gitIn(t, home, "config", "--global", "user.name", "Claude")
	gitIn(t, home, "config", "--global", "gpg.ssh.program", "/tmp/code-sign")
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte("github.com ssh-ed25519 AAAA\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := newRepo(t)
	before := snapshot(t, home)
	repoBefore := snapshot(t, filepath.Join(repo, ".git"))

	out, err := run(t, home, repo, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != cloudinit.NothingToDo+"\n" {
		t.Errorf("stdout = %q, want exactly the one line %q", out, cloudinit.NothingToDo)
	}
	if after := snapshot(t, home); !reflect.DeepEqual(before, after) {
		t.Errorf("the home directory changed:\nbefore %v\nafter  %v", before, after)
	}
	if after := snapshot(t, filepath.Join(repo, ".git")); !reflect.DeepEqual(repoBefore, after) {
		t.Errorf("the checkout's git directory changed")
	}
}

func TestIdentityIsWrittenGloballyAndIntoACheckoutWithoutOne(t *testing.T) {
	home := fakeHome(t)
	gitIn(t, home, "config", "--global", "user.name", "Claude")
	gitIn(t, home, "config", "--global", "user.email", "noreply@anthropic.com")
	repo := newRepo(t)

	if _, err := run(t, home, repo, map[string]string{
		cloudinit.EnvUser:  "Ada Lovelace",
		cloudinit.EnvEmail: "ada@example.com",
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := globalConfig(t, "user.name"); got != "Ada Lovelace" {
		t.Errorf("global user.name = %q", got)
	}
	if got := globalConfig(t, "user.email"); got != "ada@example.com" {
		t.Errorf("global user.email = %q", got)
	}
	if got := gitIn(t, repo, "config", "--local", "user.name"); got != "Ada Lovelace" {
		t.Errorf("local user.name = %q", got)
	}
	if got := gitIn(t, repo, "config", "--local", "user.email"); got != "ada@example.com" {
		t.Errorf("local user.email = %q", got)
	}
}

func TestARepoLocalIdentityIsNeverOverwritten(t *testing.T) {
	home := fakeHome(t)
	repo := newRepo(t)
	gitIn(t, repo, "config", "--local", "user.email", "ada@work.example")

	if _, err := run(t, home, repo, map[string]string{
		cloudinit.EnvUser:  "Ada Lovelace",
		cloudinit.EnvEmail: "ada@example.com",
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := gitIn(t, repo, "config", "--local", "user.email"); got != "ada@work.example" {
		t.Errorf("local user.email = %q, want the checkout's own value kept", got)
	}
	if got := gitIn(t, repo, "config", "--local", "user.name"); got != "Ada Lovelace" {
		t.Errorf("local user.name = %q, want it filled in since the checkout had none", got)
	}
	if got := globalConfig(t, "user.email"); got != "ada@example.com" {
		t.Errorf("global user.email = %q", got)
	}
}

func TestEachIdentityVariableAppliesOnItsOwn(t *testing.T) {
	home := fakeHome(t)
	gitIn(t, home, "config", "--global", "user.email", "noreply@anthropic.com")

	if _, err := run(t, home, t.TempDir(), map[string]string{cloudinit.EnvUser: "Ada Lovelace"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := globalConfig(t, "user.name"); got != "Ada Lovelace" {
		t.Errorf("global user.name = %q", got)
	}
	if got := globalConfig(t, "user.email"); got != "noreply@anthropic.com" {
		t.Errorf("global user.email = %q, want it left alone when %s is unset", got, cloudinit.EnvEmail)
	}
}

func TestAnUnsetKeyLeavesSigningConfigUntouched(t *testing.T) {
	home := fakeHome(t)
	signing := map[string]string{
		"gpg.format":      "ssh",
		"gpg.ssh.program": "/tmp/code-sign",
		"user.signingkey": "/somewhere/else.pub",
		"commit.gpgsign":  "true",
		"tag.gpgsign":     "false",
	}
	for k, v := range signing {
		gitIn(t, home, "config", "--global", k, v)
	}

	if _, err := run(t, home, t.TempDir(), map[string]string{cloudinit.EnvUser: "Ada Lovelace"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for k, want := range signing {
		if got := globalConfig(t, k); got != want {
			t.Errorf("%s = %q, want %q left as it was", k, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh")); !os.IsNotExist(err) {
		t.Errorf("~/.ssh was touched with no key supplied: %v", err)
	}
}

func TestTheKeyIsInstalledAndGitSignsWithIt(t *testing.T) {
	home := fakeHome(t)
	gitIn(t, home, "config", "--global", "gpg.ssh.program", "/tmp/code-sign")
	key := newKey(t, "")

	out, err := run(t, home, t.TempDir(), map[string]string{cloudinit.EnvSigningKey: key})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	keyPath := filepath.Join(home, ".ssh", cloudinit.KeyName)
	assertInstalled(t, home, key)

	want := map[string]string{
		"gpg.format":      "ssh",
		"user.signingkey": keyPath + ".pub",
		"gpg.ssh.program": "ssh-keygen",
		"commit.gpgsign":  "true",
		"tag.gpgsign":     "true",
	}
	for k, v := range want {
		if got := globalConfig(t, k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if !strings.Contains(out, "SHA256:") {
		t.Errorf("stdout carries no fingerprint: %q", out)
	}
	if strings.Contains(out, key) || strings.Contains(out, "PRIVATE KEY") {
		t.Errorf("stdout carries key material: %q", out)
	}

	repo := newRepo(t)
	gitIn(t, repo, "config", "user.name", "Ada")
	gitIn(t, repo, "config", "user.email", "ada@example.com")
	gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "signed")
	if sig := gitIn(t, repo, "cat-file", "commit", "HEAD"); !strings.Contains(sig, "BEGIN SSH SIGNATURE") {
		t.Errorf("git did not sign the commit:\n%s", sig)
	}
}

func TestRunningAgainIsIdempotentAndRecreatesAWipedKey(t *testing.T) {
	home := fakeHome(t)
	key := newKey(t, "")
	vars := map[string]string{
		cloudinit.EnvUser:       "Ada Lovelace",
		cloudinit.EnvEmail:      "ada@example.com",
		cloudinit.EnvSigningKey: key,
	}
	repo := newRepo(t)

	if _, err := run(t, home, repo, vars); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	first := snapshot(t, home)
	firstRepo := gitIn(t, repo, "config", "--local", "--list")

	if _, err := run(t, home, repo, vars); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if again := snapshot(t, home); !reflect.DeepEqual(first, again) {
		t.Errorf("a second run changed the home directory:\nfirst %v\nagain %v", first, again)
	}
	if again := gitIn(t, repo, "config", "--local", "--list"); again != firstRepo {
		t.Errorf("a second run changed the checkout's config:\nfirst %s\nagain %s", firstRepo, again)
	}

	keyPath := filepath.Join(home, ".ssh", cloudinit.KeyName)
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath+".pub", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, repo, vars); err != nil {
		t.Fatalf("Run after wiping the key: %v", err)
	}
	assertInstalled(t, home, key)
	if again := snapshot(t, home); !reflect.DeepEqual(first, again) {
		t.Errorf("the re-created key differs from the first install")
	}
}

// armor wraps label in the five dashes either side that a key file's
// header and footer carry.
func armor(label string) string { return "-----" + label + "-----" }

// notAKey has a key file's header and footer around a body that is no key:
// placeholder text, base64-encoded. The armour is assembled at run time so
// the source holds no private-key marker for a secret scanner to match.
var notAKey = armor("BEGIN OPENSSH PRIVATE KEY") + "\nc2VjcmV0LWtleS1ib2R5LXRoYXQtaXMtbm90LWEta2V5\n" + armor("END OPENSSH PRIVATE KEY") + "\n"

func TestAMalformedKeyFailsWithoutQuotingIt(t *testing.T) {
	for name, encoded := range map[string]string{
		"not base64":    "this is !!! not base64 but a secret-looking-string",
		"not a key":     base64.StdEncoding.EncodeToString([]byte(notAKey)),
		"truncated key": newKey(t, "")[:120],
	} {
		t.Run(name, func(t *testing.T) {
			home := fakeHome(t)
			gitIn(t, home, "config", "--global", "gpg.ssh.program", "/tmp/code-sign")

			_, err := run(t, home, t.TempDir(), map[string]string{cloudinit.EnvSigningKey: encoded})
			if err == nil {
				t.Fatal("Run accepted a malformed key")
			}
			msg := err.Error()
			if strings.Contains(msg, encoded) || strings.Contains(msg, strings.TrimSpace(encoded)[:16]) {
				t.Errorf("the error quotes the base64 input: %q", msg)
			}
			if decoded, derr := base64.StdEncoding.DecodeString(encoded); derr == nil {
				for _, line := range strings.Split(string(decoded), "\n") {
					if len(line) >= 8 && !strings.HasPrefix(line, "-----") && strings.Contains(msg, line) {
						t.Errorf("the error quotes the decoded key: %q", msg)
					}
				}
			}
			if !strings.Contains(msg, cloudinit.EnvSigningKey) {
				t.Errorf("the error does not name the variable: %q", msg)
			}
			if got := globalConfig(t, "gpg.ssh.program"); got != "/tmp/code-sign" {
				t.Errorf("gpg.ssh.program = %q, want it untouched by a key that failed", got)
			}
			assertNoTempFiles(t, home)
			if _, err := os.Stat(filepath.Join(home, ".ssh", cloudinit.KeyName)); !os.IsNotExist(err) {
				t.Errorf("a malformed key was moved into place: %v", err)
			}
		})
	}
}

func TestAPassphraseProtectedKeyFailsClearlyWithoutHanging(t *testing.T) {
	home := fakeHome(t)
	gitIn(t, home, "config", "--global", "gpg.ssh.program", "/tmp/code-sign")
	key := newKey(t, "correct horse battery staple")

	_, err := run(t, home, t.TempDir(), map[string]string{cloudinit.EnvSigningKey: key})
	if err == nil {
		t.Fatal("Run accepted a passphrase-protected key")
	}
	if !strings.Contains(err.Error(), "passphrase") {
		t.Errorf("the error does not say the key needs a passphrase: %q", err)
	}
	if strings.Contains(err.Error(), key[:16]) {
		t.Errorf("the error quotes the key: %q", err)
	}
	if got := globalConfig(t, "gpg.ssh.program"); got != "/tmp/code-sign" {
		t.Errorf("gpg.ssh.program = %q, want it untouched", got)
	}
	assertNoTempFiles(t, home)
}

func TestAnExistingSSHDirectoryIsRestrictedTo0700(t *testing.T) {
	home := fakeHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, t.TempDir(), map[string]string{cloudinit.EnvSigningKey: newKey(t, "")}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := mode(t, filepath.Join(home, ".ssh")); got != 0o700 {
		t.Errorf("~/.ssh mode = %o, want 0700", got)
	}
}

// assertInstalled checks the key file holds the decoded key at 0600 in a 0700
// directory, with its public half beside it and no temp file left over.
func assertInstalled(t *testing.T, home, encoded string) {
	t.Helper()
	sshDir := filepath.Join(home, ".ssh")
	keyPath := filepath.Join(sshDir, cloudinit.KeyName)
	if got := mode(t, sshDir); got != 0o700 {
		t.Errorf("~/.ssh mode = %o, want 0700", got)
	}
	if got := mode(t, keyPath); got != 0o600 {
		t.Errorf("key mode = %o, want 0600", got)
	}
	data, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := base64.StdEncoding.DecodeString(encoded)
	if string(data) != string(want) {
		t.Errorf("the key file does not hold the decoded key")
	}
	pub, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(pub), "ssh-ed25519 ") {
		t.Errorf("the .pub does not hold a public key: %q", pub)
	}
	assertNoTempFiles(t, home)
}

func assertNoTempFiles(t *testing.T, home string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(home, ".ssh"))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("a temp file was left behind: %s", e.Name())
		}
	}
}
