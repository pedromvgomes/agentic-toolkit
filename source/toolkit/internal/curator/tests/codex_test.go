package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-driver/agentictest"

	"github.com/pedromvgomes/agentic-toolkit/internal/curator"
)

// codexEnvelope is one `codex exec --json` stream that ends in a turn the
// decoder can fold into a Result.
const codexEnvelope = `{"type":"thread.started","thread_id":"22222222-2222-4222-8222-222222222222"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"dry run report"}}
{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":2,"reasoning_output_tokens":0}}
`

func codexOpts(t *testing.T, opts curator.Options) (curator.Options, *agentictest.Fake) {
	t.Helper()
	fake := (&agentictest.Fake{Stdout: codexEnvelope}).Build(t)
	opts.Provider = "codex"
	opts.Binary = fake.Path()
	if opts.WorkDir == "" {
		opts.WorkDir = t.TempDir()
	}
	if opts.NotesDir == "" {
		opts.NotesDir = "/repo/.memory/notes"
	}
	if opts.CandidatesDir == "" {
		opts.CandidatesDir = "/repo/.memory/candidates"
	}
	return opts, fake
}

// A run that writes notes has to be bounded to the notes directory, and a
// provider whose only vocabulary is a whole-workspace sandbox cannot express
// that. Running anyway would hand the curator write authority over the repo
// while the store's own notes claim the grant is what confines it.
func TestARealRunIsRefusedOnAProviderWithNoPerToolGrant(t *testing.T) {
	opts, fake := codexOpts(t, curator.Options{})

	_, err := curator.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("a real run on a provider with no per-tool grant was allowed")
	}
	if fake.Ran() {
		t.Error("the refusal came after the process was started")
	}
	for _, want := range []string{"codex", "dry-run"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// Check exists so that confirming the configuration does not require
// performing it. Reporting a grant the provider cannot express is the failure
// that command is for: it is a green light for a run that cannot start.
func TestCheckRefusesARealRunOnAProviderWithNoPerToolGrant(t *testing.T) {
	opts, _ := codexOpts(t, curator.Options{})

	ready, err := curator.Check(opts)
	if err == nil {
		t.Fatalf("check reported a runnable configuration: %+v", ready)
	}
	if !strings.Contains(err.Error(), "dry-run") {
		t.Errorf("error %q does not say what does work", err)
	}
}

// A preview reads and writes nothing, which a sandbox expresses exactly. On
// this provider the dry run is bounded more tightly than on one with a tool
// allowlist, because the confinement is enforced by the sandbox rather than by
// withholding tools from a list.
func TestADryRunIsBoundedBySandboxWhenThereIsNoAllowlist(t *testing.T) {
	opts, _ := codexOpts(t, curator.Options{DryRun: true})

	ready, err := curator.Check(opts)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if ready.Mode != "read-only" {
		t.Errorf("Mode = %q, want the read-only sandbox", ready.Mode)
	}
	if len(ready.Tools) != 0 {
		t.Errorf("Tools = %v, want none — this provider has no allowlist to grant", ready.Tools)
	}
}

// The curator's content policy travels in the roster entry, which a provider
// that cannot define agents never receives. Sending it anyway fails the run
// outright; dropping it silently would run the curator without the rules that
// make it a curator, so the policy moves into the prompt itself.
func TestThePolicyIsInlinedForAProviderThatCannotDefineAgents(t *testing.T) {
	opts, fake := codexOpts(t, curator.Options{DryRun: true})

	if _, err := curator.Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	argv := strings.Join(fake.Recorded(t).Args, "\x00")

	if strings.Contains(argv, "Delegate to the "+curator.AgentName) {
		t.Error("the run was told to delegate to an agent this provider cannot define")
	}
	// prompt.md's own heading, which appears nowhere else. The harness records
	// one `arg:` line per argument, so a multi-line prompt is observable only
	// by its first line — enough to tell the policy document apart from a
	// prompt that merely names the job.
	if !strings.Contains(argv, "# Memory Curator") {
		t.Errorf("the curator's policy did not reach the run: %q", argv)
	}
	if !strings.Contains(argv, "-s\x00read-only") {
		t.Errorf("the dry run was not sandboxed: %q", argv)
	}
}
