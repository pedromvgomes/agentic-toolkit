package tests

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	agentic "github.com/pedromvgomes/agentic-driver"

	"github.com/pedromvgomes/agentic-toolkit/internal/provider"
)

// mute implements only the mandatory interface: it can be driven, but it
// cannot be told what a scripted run may do. A run that must be bounded
// therefore cannot be bounded on it, and saying so is the whole point — a
// provider that silently accepted the request would run unbounded.
type mute struct{}

func (mute) Descriptor() agentic.Descriptor {
	return agentic.Descriptor{ID: "mute", DisplayName: "Mute", Binary: "mute"}
}
func (mute) StreamCommand(agentic.Request) (agentic.Invocation, error) {
	return agentic.Invocation{}, nil
}
func (mute) NewDecoder(agentic.Request) agentic.Decoder { return nil }

// broken can be told about permissions and fails for a reason that is not
// "this CLI has no vocabulary for that" — a failure to answer rather than an
// answer of no, which must not be read as the signal to fall back to a
// sandbox.
type broken struct{ mute }

var errBroken = errors.New("the permission file could not be written")

func (broken) PermissionArgs(string, []string) ([]string, error) { return nil, errBroken }

// grants takes a per-tool allowlist, the way Claude Code does.
type grants struct{ mute }

func (grants) PermissionArgs(mode string, tools []string) ([]string, error) {
	return append([]string{"--allowedTools"}, tools...), nil
}

// sandboxed refuses an allowlist and accepts a sandbox mode, the way codex
// does.
type sandboxed struct{ mute }

func (sandboxed) PermissionArgs(mode string, tools []string) ([]string, error) {
	if len(tools) > 0 {
		return nil, agentic.ErrInvalidRequest
	}
	if mode != provider.SandboxReadOnly {
		return nil, agentic.ErrInvalidRequest
	}
	return []string{"-s", mode}, nil
}

// unbounded accepts an allowlist request but no sandbox — a shape neither
// shipped provider has, and exactly the one a caller must not assume away.
type unbounded struct{ mute }

func (unbounded) PermissionArgs(mode string, tools []string) ([]string, error) {
	if len(tools) > 0 {
		return nil, agentic.ErrInvalidRequest
	}
	return nil, agentic.ErrInvalidRequest
}

var readOnlyTools = []string{"Read", "Grep"}

// There is deliberately no default provider: every operation that resolves one
// spends money and reaches outside the machine.
func TestAnUnnamedProviderIsRefused(t *testing.T) {
	if _, err := provider.New(""); !errors.Is(err, provider.ErrUnnamed) {
		t.Errorf("New(\"\") = %v, want ErrUnnamed", err)
	}
}

// A name the driver has no provider for is a gap to fill in the driver, and
// the refusal names what does exist so the fix does not need the docs.
func TestAnUnknownProviderNamesTheOnesThatExist(t *testing.T) {
	_, err := provider.New("gemini")
	if err == nil {
		t.Fatal("New accepted a provider that does not exist")
	}
	for _, want := range []string{`"gemini" is not a provider`, "claudecode, codex"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

// Every name in the shared table resolves. The list and the switch are one
// declaration or they drift, which is the failure this package exists for.
func TestEveryNameInTheTableResolves(t *testing.T) {
	for _, name := range provider.Names {
		if _, err := provider.New(name); err != nil {
			t.Errorf("New(%q): %v", name, err)
		}
	}
}

// A provider with no permission vocabulary cannot answer the question, which
// is not the same as answering no.
func TestAProviderThatCannotBeToldAnythingIsRefused(t *testing.T) {
	if _, err := provider.GrantsTools(mute{}, "", readOnlyTools); err == nil {
		t.Error("GrantsTools accepted a provider that takes no permissions")
	}
	if err := provider.AcceptsSandbox(mute{}, provider.SandboxReadOnly); err == nil {
		t.Error("AcceptsSandbox accepted a provider that takes no permissions")
	}
	if _, err := provider.ReadOnly(mute{}, readOnlyTools); err == nil {
		t.Error("ReadOnly bounded a run on a provider that cannot be bounded")
	}
}

// ErrInvalidRequest means "this CLI has no such vocabulary" and is the signal
// to try a sandbox. Any other error is a failure to answer, and treating it as
// a no would fall back to a sandbox on a provider that never said it takes one.
func TestAFailureToAnswerIsNotAnAnswerOfNo(t *testing.T) {
	granted, err := provider.GrantsTools(broken{}, "", readOnlyTools)
	if !errors.Is(err, errBroken) {
		t.Fatalf("GrantsTools = (%v, %v), want the underlying failure", granted, err)
	}
	if granted {
		t.Error("a provider that failed to answer was read as granting tools")
	}
	if _, err := provider.ReadOnly(broken{}, readOnlyTools); !errors.Is(err, errBroken) {
		t.Errorf("ReadOnly = %v, want the underlying failure rather than a sandbox fallback", err)
	}
}

// The narrower grant is preferred where a provider has one.
func TestAProviderWithAnAllowlistIsBoundedByIt(t *testing.T) {
	b, err := provider.ReadOnly(grants{}, readOnlyTools)
	if err != nil {
		t.Fatalf("ReadOnly: %v", err)
	}
	if b.Mode != "" || len(b.AllowedTools) != len(readOnlyTools) {
		t.Errorf("Bound = %+v, want the tool grant and no sandbox", b)
	}
}

// A provider without one is bounded by a sandbox, discovered by asking rather
// than by knowing which CLI it is.
func TestAProviderWithoutAnAllowlistIsBoundedByASandbox(t *testing.T) {
	b, err := provider.ReadOnly(sandboxed{}, readOnlyTools)
	if err != nil {
		t.Fatalf("ReadOnly: %v", err)
	}
	if b.Mode != provider.SandboxReadOnly || b.AllowedTools != nil {
		t.Errorf("Bound = %+v, want the read-only sandbox and no tools", b)
	}
}

// A provider that can express neither is refused, naming what it would not
// accept — rather than running with nothing bounding it at all.
func TestAProviderThatExpressesNeitherIsRefused(t *testing.T) {
	_, err := provider.ReadOnly(unbounded{}, readOnlyTools)
	if err == nil {
		t.Fatal("ReadOnly bounded a run on a provider that accepts no confinement")
	}
	for _, want := range []string{"no per-tool allowlist", provider.SandboxReadOnly} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

// Both shipped providers can express a read-only run, each in its own
// vocabulary. This is the assertion that would have failed when the driver
// changed a constructor under us.
func TestTheShippedProvidersCanBoundAReadOnlyRun(t *testing.T) {
	for _, name := range provider.Names {
		t.Run(name, func(t *testing.T) {
			p, err := provider.New(name)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			b, err := provider.ReadOnly(p, readOnlyTools)
			if err != nil {
				t.Fatalf("ReadOnly: %v", err)
			}
			if b.Mode == "" && b.AllowedTools == nil {
				t.Error("the run would be bounded by nothing at all")
			}
			// Every run in a review binds its answer to a schema; a provider
			// that cannot is refused before a process starts.
			if _, ok := p.(agentic.SchemaConstrainer); !ok {
				t.Error("provider cannot bind an answer to a schema")
			}
			if sc, ok := p.(agentic.SchemaConstrainer); ok {
				if _, err := sc.SchemaArgs(json.RawMessage(`{"type":"object"}`)); err != nil {
					t.Errorf("SchemaArgs: %v", err)
				}
			}
		})
	}
}
