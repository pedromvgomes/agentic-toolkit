package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/curator"
)

// A verification failure hands curator.Run's caller a populated Result
// alongside the error, so the curator's own account of the run — the only
// explanation of what it thought it did — must reach the operator rather
// than being discarded in favor of the bare mismatch error.
func TestReportCurateResult_VerificationFailurePrintsTheCuratorsAccount(t *testing.T) {
	var out bytes.Buffer
	env := &Env{Stdout: &out}
	res := curator.Result{
		Text:    "Promoted: pins-shas\nStore: 9 notes, 0 stale",
		Model:   "claude-opus-5",
		CostUSD: 0.42,
	}
	runErr := errors.New(`curator: note "pins-shas" is reported touched but its anchor is unstamped`)

	err := reportCurateResult(env, false, false, res, runErr)

	if !errors.Is(err, runErr) {
		t.Errorf("error = %v, want the verification error returned", err)
	}
	if !strings.Contains(out.String(), "Promoted: pins-shas") {
		t.Errorf("stdout = %q, want the curator's own report", out.String())
	}
}

// A config or setup error (no provider, missing binary, no completion
// report at all) returns before curator.Run ever populates a Result, so
// there is nothing of the curator's to print — only the error.
func TestReportCurateResult_SetupErrorPrintsNothing(t *testing.T) {
	var out bytes.Buffer
	env := &Env{Stdout: &out}
	runErr := errors.New("curator: run produced no completion report")

	err := reportCurateResult(env, false, false, curator.Result{}, runErr)

	if !errors.Is(err, runErr) {
		t.Errorf("error = %v, want the setup error returned", err)
	}
	if out.String() != "" {
		t.Errorf("stdout = %q, want nothing printed for an empty Result", out.String())
	}
}

// --json carries the same distinction: a verification failure's report is
// still written, with failed:true, rather than only surfacing as a non-zero
// exit with no explanation in the machine-readable output.
func TestReportCurateResult_VerificationFailureJSONMarksFailed(t *testing.T) {
	var out bytes.Buffer
	env := &Env{Stdout: &out}
	res := curator.Result{Text: "could not verify the run", Model: "claude-opus-5"}
	runErr := errors.New("curator: candidate not resolved")

	if err := reportCurateResult(env, true, false, res, runErr); !errors.Is(err, runErr) {
		t.Errorf("error = %v, want the verification error returned", err)
	}
	for _, want := range []string{`"failed": true`, `"report": "could not verify the run"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("json output = %q, missing %q", out.String(), want)
		}
	}
}

// A successful run with no error and no IsError is the ordinary case: the
// report prints and nothing is returned.
func TestReportCurateResult_SuccessPrintsAndReturnsNil(t *testing.T) {
	var out bytes.Buffer
	env := &Env{Stdout: &out}
	res := curator.Result{Text: "Store: 9 notes, 0 stale"}

	if err := reportCurateResult(env, false, false, res, nil); err != nil {
		t.Errorf("error = %v, want nil for a clean run", err)
	}
	if !strings.Contains(out.String(), "Store: 9 notes, 0 stale") {
		t.Errorf("stdout = %q, want the curator's report", out.String())
	}
}

// The curator's own verdict that its turn failed is not a Go error from
// running it, but still has to fail the command — that is what
// errMemoryCurate is for.
func TestReportCurateResult_CuratorsOwnFailureReturnsSentinel(t *testing.T) {
	var out bytes.Buffer
	env := &Env{Stdout: &out}
	res := curator.Result{Text: "could not reach the store", IsError: true}

	if err := reportCurateResult(env, false, false, res, nil); !errors.Is(err, errMemoryCurate) {
		t.Errorf("error = %v, want errMemoryCurate", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

// The JSON report is the only place the curator's own account reaches a
// script, so a write that fails has to surface instead of vanishing behind
// the verification error it was meant to accompany.
func TestReportCurateResult_JSONWriteFailureIsReturned(t *testing.T) {
	env := &Env{Stdout: failingWriter{}}
	res := curator.Result{Text: "curated"}

	err := reportCurateResult(env, true, false, res, errors.New("curator: mismatch"))

	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Errorf("error = %v, want the write failure", err)
	}
}

func TestReportCurateCheck_TextNamesEveryPartOfTheGrant(t *testing.T) {
	var out bytes.Buffer
	env := &Env{Stdout: &out}
	ready := curator.Ready{
		Provider:        "claudecode",
		Binary:          "/opt/claude",
		Tools:           []string{"Read", "Grep"},
		DisallowedTools: []string{"Agent", "Task"},
	}

	if err := reportCurateCheck(env, false, ready); err != nil {
		t.Fatalf("reportCurateCheck: %v", err)
	}
	for _, want := range []string{"claudecode", "/opt/claude", "none passed", "Read, Grep", "deny:      Agent, Task"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("--check output is missing %q:\n%s", want, out.String())
		}
	}
}

func TestReportCurateCheck_JSONCarriesTheDenyList(t *testing.T) {
	var out bytes.Buffer
	env := &Env{Stdout: &out}

	err := reportCurateCheck(env, true, curator.Ready{Provider: "claudecode", DisallowedTools: []string{"Agent", "Task"}})

	if err != nil {
		t.Fatalf("reportCurateCheck: %v", err)
	}
	if !strings.Contains(out.String(), `"disallowed_tools"`) || !strings.Contains(out.String(), `"Agent"`) {
		t.Errorf("json output does not carry the deny list:\n%s", out.String())
	}
	if err := reportCurateCheck(&Env{Stdout: failingWriter{}}, true, curator.Ready{}); err == nil {
		t.Error("a JSON write failure was swallowed")
	}
}

// A provider with no per-tool vocabulary has no deny list, and a blank field
// there would read as nothing confining the run at all.
func TestDescribeDenyList_EmptyIsSaidPlainly(t *testing.T) {
	if got := describeDenyList(nil); !strings.Contains(got, "cannot deny a tool") {
		t.Errorf("describeDenyList(nil) = %q, want the empty case explained", got)
	}
	if got := describeDenyList([]string{"Agent", "Task"}); got != "Agent, Task" {
		t.Errorf("describeDenyList = %q, want the tools listed", got)
	}
}
