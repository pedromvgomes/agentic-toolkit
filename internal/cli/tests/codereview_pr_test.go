package tests

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/cli"
	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

// runCLIWithStdin is runCLI with something on standard input, which is how a
// private key arrives when it comes from a password manager rather than a
// file.
func runCLIWithStdin(t *testing.T, workDir, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	env := &cli.Env{
		Stdin:   strings.NewReader(stdin),
		Stdout:  &outBuf,
		Stderr:  &errBuf,
		WorkDir: workDir,
	}
	root := cli.NewRootCmd(env)
	root.SetArgs(args)
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// keyPEM is a private key in the form GitHub hands out.
func keyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
}

// isolatedConfig points agtk's config directory at a temporary one, so a test
// never reads or writes the operator's own registration.
func isolatedConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return filepath.Join(dir, "agentic-toolkit")
}

// The registration is once per machine, and the command that makes it has to
// say where it put the key: it is the file a person has to protect, back up
// and eventually rotate.
func TestInitializeRegistersTheAppAndSaysWhereTheKeyIs(t *testing.T) {
	config := isolatedConfig(t)
	work := t.TempDir()
	keyFile := filepath.Join(work, "app.pem")
	if err := os.WriteFile(keyFile, []byte(keyPEM(t)), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runCLI(t, work, "code-review", "initialize", "--app-id", "4242", "--key-file", keyFile)
	if err != nil {
		t.Fatalf("initialize: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, filepath.Join(config, githubapp.KeyFile)) {
		t.Errorf("initialize does not say where the key went:\n%s", stdout)
	}
	cred, err := githubapp.Load(config)
	if err != nil {
		t.Fatalf("the registration does not load: %v", err)
	}
	if cred.AppID != 4242 {
		t.Errorf("the registered App is %d", cred.AppID)
	}
	info, err := os.Stat(filepath.Join(config, githubapp.KeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != githubapp.FileMode {
		t.Errorf("the key is mode %04o, want %04o: the blast radius of this key is one machine's one user", got, githubapp.FileMode)
	}
}

// A key that passes through a temporary file is a key that outlives the
// command that used it.
func TestInitializeTakesTheKeyOnStandardInput(t *testing.T) {
	config := isolatedConfig(t)
	_, stderr, err := runCLIWithStdin(t, t.TempDir(), keyPEM(t),
		"code-review", "initialize", "--app-id", "8", "--key-stdin")
	if err != nil {
		t.Fatalf("initialize: %v\n%s", err, stderr)
	}
	if _, err := githubapp.Load(config); err != nil {
		t.Fatalf("a key given on standard input did not register: %v", err)
	}
}

// A registration that is half specified is a registration nobody can use, and
// the refusal has to name what is missing rather than fail later against
// GitHub.
func TestInitializeRefusesAnIncompleteRegistration(t *testing.T) {
	isolatedConfig(t)
	work := t.TempDir()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"code-review", "initialize"}, "--app-id"},
		{[]string{"code-review", "initialize", "--app-id", "5"}, "--key-file"},
		{[]string{"code-review", "initialize", "--app-id", "5", "--key-file", "x.pem", "--key-stdin"}, "pass one"},
	} {
		_, _, err := runCLI(t, work, tc.args...)
		if err == nil {
			t.Errorf("%v was accepted", tc.args)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v refused without naming %q: %v", tc.args, tc.want, err)
		}
	}
}

// A preview that wrote the key would not be a preview.
func TestInitializeDryRunWritesNothing(t *testing.T) {
	config := isolatedConfig(t)
	stdout, _, err := runCLI(t, t.TempDir(), "code-review", "initialize",
		"--app-id", "11", "--key-stdin", "--dry-run")
	if err != nil {
		t.Fatalf("initialize --dry-run: %v", err)
	}
	if !strings.Contains(stdout, githubapp.KeyFile) {
		t.Errorf("the preview does not name the key file:\n%s", stdout)
	}
	if _, err := os.Stat(config); err == nil {
		t.Error("a preview created the config directory")
	}
}

// A key any other account can read makes "the blast radius is one machine"
// untrue, and using it anyway would leave the refusal in the documentation
// only.
func TestAReviewRefusesToRunOnAWorldReadableKey(t *testing.T) {
	config := isolatedConfig(t)
	work := gitProject(t, nil)
	runGit(t, work, "remote", "add", "origin", "git@github.com:acme/widgets.git")

	if err := githubapp.Initialize(config, 4242, []byte(keyPEM(t))); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(config, githubapp.KeyFile), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := runCLI(t, work, "code-review", "run", "--pr", "7", "--dry-run")
	if err == nil {
		t.Fatal("a review ran on a key every account on this machine can read")
	}
	if !strings.Contains(err.Error(), "chmod") {
		t.Errorf("the refusal does not say how to fix it: %v", err)
	}
}

// A machine that never registered is the first thing anybody hits, and the
// answer is one command.
func TestAReviewOfAPullRequestSaysHowToRegisterTheApp(t *testing.T) {
	isolatedConfig(t)
	work := gitProject(t, nil)
	runGit(t, work, "remote", "add", "origin", "git@github.com:acme/widgets.git")

	_, _, err := runCLI(t, work, "code-review", "run", "--pr", "7")
	if err == nil {
		t.Fatal("a review was posted from a machine holding no registration")
	}
	if !strings.Contains(err.Error(), "code-review initialize") {
		t.Errorf("the refusal does not name the command that fixes it: %v", err)
	}
}

// A pull request decides the base, the head and the context. A second answer
// for any of them would mean reviewing one change and posting the result to
// another.
func TestAPullRequestTargetRefusesASecondAnswerForTheSameThing(t *testing.T) {
	isolatedConfig(t)
	work := gitProject(t, nil)
	for _, args := range [][]string{
		{"code-review", "run", "--pr", "7", "--base", "main"},
		{"code-review", "run", "--pr", "7", "--head", "HEAD"},
	} {
		_, _, err := runCLI(t, work, args...)
		if err == nil {
			t.Errorf("%v was accepted", args)
			continue
		}
		if !strings.Contains(err.Error(), "--pr names the change") {
			t.Errorf("%v refused for the wrong reason: %v", args, err)
		}
	}
}

// The two flags withhold different things, and one command cannot mean both.
func TestDryRunAndNoPostAreNotTheSameRequest(t *testing.T) {
	isolatedConfig(t)
	work := gitProject(t, nil)
	_, _, err := runCLI(t, work, "code-review", "run", "--pr", "7", "--dry-run", "--no-post")
	if err == nil || !strings.Contains(err.Error(), "pass one") {
		t.Fatalf("--dry-run and --no-post together were accepted: %v", err)
	}
}

func TestAPullRequestNumberBelowOneIsRefusedByTheCLI(t *testing.T) {
	isolatedConfig(t)
	work := gitProject(t, nil)
	_, _, err := runCLI(t, work, "code-review", "run", "--pr", "-3")
	if err == nil || !strings.Contains(err.Error(), "not a pull request number") {
		t.Fatalf("-3 was accepted as a pull request number: %v", err)
	}
}

// A repository whose remote is not an owner/repository address has nowhere to
// post to, and guessing would post to a repository nobody named.
func TestAReviewRefusesARemoteItCannotAddressAReviewTo(t *testing.T) {
	isolatedConfig(t)
	work := gitProject(t, nil)
	runGit(t, work, "remote", "add", "origin", "/srv/git/mirror.git")
	_, _, err := runCLI(t, work, "code-review", "run", "--pr", "7")
	if err == nil || !strings.Contains(err.Error(), "owner/repository") {
		t.Fatalf("a remote that names no repository was accepted: %v", err)
	}
}

// A flag that is silently ignored reads as a flag that was honoured — which on
// this one means believing a review was withheld from a pull request that was
// never named.
func TestNoPostIsRefusedWithoutAPullRequest(t *testing.T) {
	work := gitProject(t, nil)
	_, _, err := runCLI(t, work, "code-review", "run", "--no-post")
	if err == nil {
		t.Fatal("--no-post was accepted with no pull request to withhold a post from")
	}
	if !strings.Contains(err.Error(), "nothing to withhold") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}
