package tests

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-driver/agentictest"

	"github.com/pedromvgomes/agentic-toolkit/internal/curator"
)

// A finished `claude -p … --output-format json` turn, trimmed to the fields
// the provider reads.
const curatedEnvelope = `{
  "type": "result",
  "subtype": "success",
  "is_error": false,
  "session_id": "s1",
  "num_turns": 4,
  "total_cost_usd": 0.42,
  "result": "Promoted: lockfile-pins-shas-not-tags\nRejected: 20260905-where-render-lives — re-derivable\nStore: 9 notes, 0 stale",
  "modelUsage": {
    "claude-opus-5[1m]": {"canonicalModel": "claude-opus-5", "inputTokens": 12, "cacheReadInputTokens": 9000}
  }
}`

// A failing turn. The CLI reporting its own failure is a verdict, not an
// outage: the report is populated and carries the explanation.
const refusedEnvelope = `{
  "type": "result",
  "subtype": "success",
  "is_error": true,
  "session_id": "s2",
  "result": "could not reach the store"
}`

func run(t *testing.T, stdout string, opts curator.Options) (*agentictest.Fake, curator.Result, error) {
	t.Helper()

	fake := (&agentictest.Fake{Stdout: stdout}).Build(t)
	opts.Binary = fake.Path()
	if opts.Provider == "" {
		opts.Provider = "claudecode"
	}
	if opts.WorkDir == "" {
		opts.WorkDir = t.TempDir()
	}
	res, err := curator.Run(t.Context(), opts)
	return fake, res, err
}

func TestARunReturnsTheCuratorsReport(t *testing.T) {
	_, res, err := run(t, curatedEnvelope, curator.Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Text, "Promoted: lockfile-pins-shas-not-tags") {
		t.Errorf("Text = %q, want the curator's report", res.Text)
	}
	if res.IsError {
		t.Error("a successful turn was reported as a failure")
	}
	if res.CostUSD != 0.42 {
		t.Errorf("CostUSD = %v, want what the provider reported", res.CostUSD)
	}
	if res.Model == "" {
		t.Error("Model is empty; the run's cost is meaningless without the model it was charged for")
	}
}

// The CLI declaring the turn a failure is a verdict from the provider, not an
// error from running it. Discarding the report as an outage loses the only
// explanation there is.
func TestACuratorsOwnFailureCarriesItsReport(t *testing.T) {
	_, res, err := run(t, refusedEnvelope, curator.Options{})
	if err != nil {
		t.Fatalf("Run turned a verdict into an error: %v", err)
	}
	if !res.IsError {
		t.Error("a failed turn was reported as a success")
	}
	if !strings.Contains(res.Text, "could not reach the store") {
		t.Errorf("Text = %q, want the explanation kept", res.Text)
	}
}

// The grant is constructed here and passed on the command line, which is what
// makes the single-writer rule enforcement rather than instruction. If it
// stopped reaching the child, nothing else would notice.
func TestTheGrantAndTheRosterReachTheChild(t *testing.T) {
	fake, _, err := run(t, curatedEnvelope, curator.Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	argv := strings.Join(fake.Recorded(t).Args, "\x00")
	for _, want := range []string{
		"--agents",
		curator.AgentName,
		"--allowedTools",
		"Bash(agtk memory anchor*)",
		"--permission-mode",
		curator.PermissionMode(),
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("the child was not given %q", want)
		}
	}

	// The measure that closes apiKeyHelper. A run that loaded settings files
	// would also be a run whose grant a settings file could widen.
	if !strings.Contains(argv, "--setting-sources") {
		t.Error("the run did not refuse to load settings sources")
	}
}

// --stale is its own command rather than a flag on audit, and the two ask for
// different work. A flag that reached the child identically would mean the
// sweep never happened.
func TestTheStaleSweepAsksForDifferentWork(t *testing.T) {
	backlog, _, err := run(t, curatedEnvelope, curator.Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	sweep, _, err := run(t, curatedEnvelope, curator.Options{Stale: true})
	if err != nil {
		t.Fatalf("Run --stale: %v", err)
	}

	backlogArgs := strings.Join(backlog.Recorded(t).Args, " ")
	sweepArgs := strings.Join(sweep.Recorded(t).Args, " ")
	if backlogArgs == sweepArgs {
		t.Fatal("--stale sent the child the same instruction as the default run")
	}
	if !strings.Contains(sweepArgs, "audit") {
		t.Errorf("the sweep does not point the curator at the stale list: %q", sweepArgs)
	}
	if !strings.Contains(backlogArgs, "candidates") {
		t.Errorf("the default run does not point the curator at the backlog: %q", backlogArgs)
	}
}

// The curator resolves the store by running agtk, so it has to start where
// agtk would have — the project root, not wherever the invoking shell sat.
func TestTheChildRunsInTheProjectRoot(t *testing.T) {
	dir := t.TempDir()
	fake, _, err := run(t, curatedEnvelope, curator.Options{WorkDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Resolved on both sides: a temp directory reaches the child through a
	// symlink on macOS, so the raw strings differ for the same directory.
	if got, want := resolve(t, fake.Recorded(t).Cwd), resolve(t, dir); got != want {
		t.Errorf("child ran in %q, want the project root %q", got, want)
	}
}

func resolve(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve %s: %v", path, err)
	}
	return real
}

// A provider the repo has not chosen must not reach a process at all.
func TestAnUnconfiguredRunStartsNoProcess(t *testing.T) {
	fake := (&agentictest.Fake{Stdout: curatedEnvelope}).Build(t)

	_, err := curator.Run(t.Context(), curator.Options{
		Binary:  fake.Path(),
		WorkDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("a run with no provider configured succeeded")
	}
	if fake.Ran() {
		t.Error("a run with no provider configured still spawned a process")
	}
}
