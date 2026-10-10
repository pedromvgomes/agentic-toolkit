package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/cloudinit"
)

// cloudInitHook returns the SessionStart entry and command the default stack
// renders for the cloud-init hook.
func cloudInitHook(t *testing.T) (matcher, command string) {
	t.Helper()

	apply := renderDefaultStack(t)
	var settings struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(readRendered(t, apply, ".claude/settings.json")), &settings); err != nil {
		t.Fatalf("settings.json: %v", err)
	}
	for _, entry := range settings.Hooks["SessionStart"] {
		for _, h := range entry.Hooks {
			if strings.Contains(h.Command, "agtk cloud init --render") {
				return entry.Matcher, h.Command
			}
		}
	}
	t.Fatal("the default stack renders no SessionStart hook that runs `agtk cloud init --render`")
	return "", ""
}

// hookRun is one execution of the hook's command under a controlled environment.
type hookRun struct {
	stdout, stderr string
	exit           int
	home           string
	calls          string
}

// runCloudInitHook runs command under `sh -c` with an environment built from
// scratch: a temporary HOME, none of the AGTK_* variables beyond vars, no
// GIT_CONFIG_GLOBAL, and a PATH holding only the tools the hook needs plus the
// stub agtk, if any. The machine's own identity, key and agtk are unreachable.
func runCloudInitHook(t *testing.T, command, stub string, vars ...string) hookRun {
	t.Helper()

	bin := t.TempDir()
	for _, tool := range []string{"grep", "sed", "cat"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("%s not on PATH: %v", tool, err)
		}
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	scratch := t.TempDir()
	calls := filepath.Join(scratch, "calls")
	if stub != "" {
		script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + calls + "'\n" + stub
		if err := os.WriteFile(filepath.Join(bin, "agtk"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	home := t.TempDir()

	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append([]string{"HOME=" + home, "PATH=" + bin}, vars...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	run := hookRun{home: home}
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run hook: %v", err)
		}
		run.exit = ee.ExitCode()
	}
	run.stdout, run.stderr = stdout.String(), stderr.String()
	if b, err := os.ReadFile(calls); err == nil {
		run.calls = string(b)
	}
	return run
}

// capableAgtk answers the --help probe the way a binary that has the flag
// does, and succeeds for the real call.
const capableAgtk = `if [ "$3" = "--help" ]; then
	printf 'Usage: agtk cloud init [flags]\n\n      --render   render checkouts\n'
	exit 0
fi
exit 0
`

func TestTheDefaultStackRendersTheCloudInitSessionStartHook(t *testing.T) {
	matcher, _ := cloudInitHook(t)
	if matcher != "startup|resume|clear|compact" {
		t.Errorf("the hook's matcher is %q", matcher)
	}
}

func TestTheCloudInitHookDoesNothingWhenNoVariableIsSet(t *testing.T) {
	_, command := cloudInitHook(t)

	for _, tc := range []struct {
		name string
		vars []string
	}{
		{"unset", nil},
		{"empty", []string{"AGTK_GH_USER=", "AGTK_GH_EMAIL=", "AGTK_SIGNING_KEY_B64="}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := runCloudInitHook(t, command, capableAgtk, tc.vars...)
			if run.exit != 0 {
				t.Errorf("exit %d, want 0", run.exit)
			}
			if want := cloudinit.NothingToDo + "\n"; run.stdout != want {
				t.Errorf("stdout %q, want %q", run.stdout, want)
			}
			if run.stderr != "" {
				t.Errorf("stderr %q, want empty", run.stderr)
			}
			if run.calls != "" {
				t.Errorf("agtk was invoked: %q", run.calls)
			}
			for _, name := range []string{".gitconfig", ".ssh"} {
				if _, err := os.Stat(filepath.Join(run.home, name)); err == nil {
					t.Errorf("%s was created in HOME", name)
				}
			}
		})
	}
}

func TestTheCloudInitHookIsSilentWithoutAnAgtk(t *testing.T) {
	_, command := cloudInitHook(t)

	run := runCloudInitHook(t, command, "", "AGTK_GH_USER=x")
	if run.exit != 0 || run.stdout != "" {
		t.Errorf("exit %d, stdout %q; want exit 0 and no output", run.exit, run.stdout)
	}
}

func TestTheCloudInitHookIsSilentWithAnAgtkThatLacksTheFlag(t *testing.T) {
	_, command := cloudInitHook(t)

	const older = `printf 'Usage: agtk cloud <command>\n\nCommands:\n  other\n'
exit 0
`
	run := runCloudInitHook(t, command, older, "AGTK_GH_USER=x")
	if run.exit != 0 || run.stdout != "" {
		t.Errorf("exit %d, stdout %q; want exit 0 and no output", run.exit, run.stdout)
	}
	if strings.Contains(run.calls, "cloud init --render") {
		t.Errorf("the real command ran against an agtk without the flag: %q", run.calls)
	}
}

func TestTheCloudInitHookRunsTheRenderWhenConfigured(t *testing.T) {
	_, command := cloudInitHook(t)

	run := runCloudInitHook(t, command, capableAgtk, "AGTK_GH_USER=x")
	if run.exit != 0 || run.stdout != "" {
		t.Errorf("exit %d, stdout %q; want exit 0 and no output", run.exit, run.stdout)
	}
	if !strings.Contains(run.calls, "cloud init --render\n") {
		t.Errorf("the render did not run: %q", run.calls)
	}
}

func TestTheCloudInitHookReportsAFailureAndStaysOpen(t *testing.T) {
	_, command := cloudInitHook(t)

	const failing = `if [ "$3" = "--help" ]; then
	printf '      --render\n'
	exit 0
fi
printf 'ssh-keygen: boom\n' >&2
exit 3
`
	run := runCloudInitHook(t, command, failing, "AGTK_GH_USER=x")
	if run.exit != 0 {
		t.Errorf("exit %d; a failing setup must not block the session", run.exit)
	}
	if !strings.Contains(run.stdout, "ssh-keygen: boom") {
		t.Errorf("the failure never reached stdout: %q", run.stdout)
	}
}
