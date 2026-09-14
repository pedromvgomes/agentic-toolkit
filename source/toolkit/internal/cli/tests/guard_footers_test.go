package tests

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/cli"
)

func TestGuardFootersDeniesCommitWithFooter(t *testing.T) {
	payload := `{"tool_name":"Bash","tool_input":{"command":"git commit -m \"subject\n\nCo-Authored-By: Someone <s@example.com>\""},"cwd":"/tmp"}`
	var outBuf, errBuf bytes.Buffer
	env := &cli.Env{
		Stdin:   strings.NewReader(payload),
		Stdout:  &outBuf,
		Stderr:  &errBuf,
		WorkDir: t.TempDir(),
	}

	code := cli.ExecuteArgs(env, []string{"guard", "footers"})

	if code != cli.GuardFootersExitCode {
		t.Fatalf("exit code = %d, want %d", code, cli.GuardFootersExitCode)
	}
	if !strings.Contains(errBuf.String(), "Co-Authored-By") {
		t.Fatalf("stderr = %q, want it to name the matched pattern", errBuf.String())
	}
}

func TestGuardFootersAllowsCleanCommit(t *testing.T) {
	payload := `{"tool_name":"Bash","tool_input":{"command":"git commit -m subject"},"cwd":"/tmp"}`
	var outBuf, errBuf bytes.Buffer
	env := &cli.Env{
		Stdin:   strings.NewReader(payload),
		Stdout:  &outBuf,
		Stderr:  &errBuf,
		WorkDir: t.TempDir(),
	}

	code := cli.ExecuteArgs(env, []string{"guard", "footers"})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, errBuf.String())
	}
}
