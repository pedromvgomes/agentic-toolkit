package tests

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/cli"
	"github.com/pedromvgomes/agentic-toolkit/internal/cloudinit"
	"github.com/pedromvgomes/agentic-toolkit/internal/updatecheck"
)

// clearCloudInitEnv leaves every AGTK_* variable cloud init reads empty,
// which it treats as unset.
func clearCloudInitEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{cloudinit.EnvUser, cloudinit.EnvEmail, cloudinit.EnvSigningKey} {
		t.Setenv(k, "")
	}
}

// isolateGitConfig points git's global config at a fresh HOME and keeps the
// system config out, so a test reads and writes nothing of the machine's.
func isolateGitConfig(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	if err := os.Unsetenv("GIT_CONFIG_GLOBAL"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return home
}

// recordUpdateCheck installs a StartUpdateCheck that counts its calls and
// starts nothing.
func recordUpdateCheck(env *cli.Env) *int {
	calls := 0
	env.StartUpdateCheck = func(*cli.Env) <-chan updatecheck.UpdateInfo {
		calls++
		return nil
	}
	return &calls
}

func runCloud(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	env := &cli.Env{
		Stdin:   strings.NewReader(""),
		Stdout:  &outBuf,
		Stderr:  &errBuf,
		WorkDir: t.TempDir(),
	}
	recordUpdateCheck(env)
	code = cli.ExecuteArgs(env, args)
	return code, outBuf.String(), errBuf.String()
}

func TestCloudInitWithNothingSetPrintsOneLineAndSucceeds(t *testing.T) {
	clearCloudInitEnv(t)
	isolateGitConfig(t)

	code, stdout, stderr := runCloud(t, "cloud", "init")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr)
	}
	if stdout != cloudinit.NothingToDo+"\n" {
		t.Errorf("stdout = %q, want exactly %q", stdout, cloudinit.NothingToDo+"\n")
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want it empty", stderr)
	}
}

func TestCloudInitWritesTheIdentityToGlobalGitConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required: %v", err)
	}
	clearCloudInitEnv(t)
	home := isolateGitConfig(t)
	t.Setenv(cloudinit.EnvUser, "Octo Cat")
	t.Setenv(cloudinit.EnvEmail, "octo@example.com")

	code, stdout, stderr := runCloud(t, "cloud", "init")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	for key, want := range map[string]string{"user.name": "Octo Cat", "user.email": "octo@example.com"} {
		out, err := exec.Command("git", "config", "--file", filepath.Join(home, ".gitconfig"), "--get", key).Output()
		if err != nil {
			t.Fatalf("read %s from the fake HOME's .gitconfig: %v", key, err)
		}
		if got := strings.TrimSpace(string(out)); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestCloudRejectsAnUnknownSubcommand(t *testing.T) {
	clearCloudInitEnv(t)

	code, stdout, _ := runCloud(t, "cloud", "no-such-subcommand")

	if code == 0 {
		t.Fatalf("an unknown subcommand succeeded and printed:\n%s", stdout)
	}
}

func TestCloudAlonePrintsHelpNamingInit(t *testing.T) {
	clearCloudInitEnv(t)

	code, stdout, stderr := runCloud(t, "cloud")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "agtk cloud [command]") || !strings.Contains(stdout, "init ") {
		t.Errorf("stdout is not the cloud command's help naming init:\n%s", stdout)
	}
}

func TestCloudInitFailurePrintsItsMessageOnceWithoutTheKey(t *testing.T) {
	clearCloudInitEnv(t)
	isolateGitConfig(t)
	const malformed = "not*valid*base64*secretmarker"
	t.Setenv(cloudinit.EnvSigningKey, malformed)

	code, stdout, stderr := runCloud(t, "cloud", "init")

	if code == 0 {
		t.Fatalf("a malformed signing key succeeded; stdout=%q stderr=%q", stdout, stderr)
	}
	all := stdout + stderr
	const msg = cloudinit.EnvSigningKey + " is not valid base64"
	if n := strings.Count(all, msg); n != 1 {
		t.Errorf("the failure appears %d times, want exactly once:\nstdout=%q\nstderr=%q", n, stdout, stderr)
	}
	if lines := strings.Split(strings.TrimRight(stderr, "\n"), "\n"); len(lines) != 1 {
		t.Errorf("stderr carries %d lines, want the one failure line alone:\n%s", len(lines), stderr)
	}
	if strings.Contains(all, "secretmarker") {
		t.Errorf("the output echoes the signing key's encoding:\nstdout=%q\nstderr=%q", stdout, stderr)
	}
}

// A session-start hook runs `agtk cloud init` in every session, so it starts
// no background update check; `agtk init` shares its name and still does.
func TestCloudInitStartsNoUpdateCheckWhileInitStillDoes(t *testing.T) {
	clearCloudInitEnv(t)
	isolateGitConfig(t)

	for _, tc := range []struct {
		args []string
		want int
	}{
		{[]string{"init"}, 1},
		{[]string{"cloud", "init"}, 0},
	} {
		var outBuf, errBuf bytes.Buffer
		env := &cli.Env{
			Stdin:   strings.NewReader(""),
			Stdout:  &outBuf,
			Stderr:  &errBuf,
			WorkDir: t.TempDir(),
		}
		calls := recordUpdateCheck(env)

		if code := cli.ExecuteArgs(env, tc.args); code != 0 {
			t.Fatalf("agtk %s: exit code = %d; stderr=%q", strings.Join(tc.args, " "), code, errBuf.String())
		}
		if *calls != tc.want {
			t.Errorf("agtk %s started the update check %d times, want %d", strings.Join(tc.args, " "), *calls, tc.want)
		}
	}
}

func TestCloudInitHelpNamesTheRenderFlags(t *testing.T) {
	clearCloudInitEnv(t)

	code, stdout, stderr := runCloud(t, "cloud", "init", "--help")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr)
	}
	if !regexp.MustCompile(`--render +run 'agtk render'`).MatchString(stdout) {
		t.Errorf("`agtk cloud init --help` does not list --render as a bare flag:\n%s", stdout)
	}
	for _, want := range []string{"--render-root string", `(default "` + cloudinit.DefaultRenderRoot + `")`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("`agtk cloud init --help` does not mention %q:\n%s", want, stdout)
		}
	}
}

// The render root holds no checkout, so the flags reach cloudinit.Run
// without starting a render: under `go test` the binary a render runs is the
// test binary itself.
func TestCloudInitRenderSearchesTheGivenRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required: %v", err)
	}
	clearCloudInitEnv(t)
	isolateGitConfig(t)
	t.Setenv(cloudinit.EnvUser, "Octo Cat")
	root := t.TempDir()

	code, stdout, stderr := runCloud(t, "cloud", "init", "--render", "--render-root", root)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	if want := "agtk cloud init: no checkout under " + root + " to render\n"; !strings.HasSuffix(stdout, want) {
		t.Errorf("stdout =\n%s\nwant it to end %q", stdout, want)
	}
}

func TestCloudInitRenderWithNothingSetPrintsOnlyTheOneLine(t *testing.T) {
	clearCloudInitEnv(t)
	isolateGitConfig(t)

	code, stdout, stderr := runCloud(t, "cloud", "init", "--render", "--render-root", t.TempDir())

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr)
	}
	if stdout != cloudinit.NothingToDo+"\n" {
		t.Errorf("stdout = %q, want exactly %q", stdout, cloudinit.NothingToDo+"\n")
	}
}

// A render's failures go to stderr, so a root that cannot be listed is
// reported there once and the exit code is non-zero.
func TestCloudInitRenderFailureExitsNonZero(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required: %v", err)
	}
	clearCloudInitEnv(t)
	isolateGitConfig(t)
	t.Setenv(cloudinit.EnvUser, "Octo Cat")
	root := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(root, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runCloud(t, "cloud", "init", "--render", "--render-root", root)

	if code == 0 {
		t.Fatalf("an unlistable render root succeeded; stdout=%q", stdout)
	}
	if !strings.HasPrefix(stderr, "agtk: cloud init: list "+root+": ") || strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want the one failure line", stderr)
	}
}
