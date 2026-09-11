// Package provider resolves a configured provider name to an agentic-driver
// provider, and answers what that provider can be told about a scripted run.
//
// It exists so the names `memory.agent` accepts and the names a reviewer's
// `provider:` accepts are one list. Two copies of the switch drift, and the
// drift is silent in both directions: a name one subsystem knows and the other
// does not reads as a typo in the manifest rather than as a gap in the
// toolkit, and a driver upgrade that changes a constructor is applied to
// whichever copy the change happened to be looking at.
//
// What it deliberately does not own is policy. How tightly a run must be
// bounded is a fact about that run — a reviewer only ever reads, while a
// curation run that writes notes cannot be expressed by a sandbox at all — so
// the primitives here answer what a provider can express and each caller
// decides what it needs.
//
// Resolving a name constructs nothing and touches no PATH. Building a driver
// is the caller's job, and only a caller that is about to spend money does it.
package provider

import (
	"errors"
	"fmt"
	"strings"

	agentic "github.com/pedromvgomes/agentic-driver"
	"github.com/pedromvgomes/agentic-driver/claudecode"
	"github.com/pedromvgomes/agentic-driver/codex"
)

// Names are the provider names agtk accepts, in the order help lists them.
var Names = []string{"claudecode", "codex"}

// ErrUnnamed means no provider was configured. There is deliberately no
// default: every operation that resolves a provider is one that spends money
// and reaches outside the machine, so an unconfigured caller gets a refusal
// rather than a guess about which CLI it meant.
var ErrUnnamed = errors.New("no provider configured")

// New resolves a name to a provider.
//
// Both are taken from PATH rather than vendored: agtk runs on a developer's
// machine against the CLI they are already authenticated with, not against a
// build this repo would have to pin.
//
// A name the driver has no provider for is a gap to fill in the driver, where
// the dialect knowledge is tested, rather than an escape hatch here.
func New(name string) (agentic.Provider, error) {
	switch name {
	case "":
		return nil, ErrUnnamed
	case "claudecode":
		return claudecode.NewOnPath()
	case "codex":
		return codex.NewOnPath()
	default:
		return nil, fmt.Errorf("%q is not a provider; use one of %s", name, strings.Join(Names, ", "))
	}
}

// Bound is how one run is confined, in the vocabulary its provider has. The
// two fields are alternatives rather than layers: a provider that takes a
// per-tool allowlist is bounded by the list, and one that does not is bounded
// by a sandbox mode.
type Bound struct {
	// Mode is a sandbox mode, or "" for the CLI's own default.
	Mode string
	// AllowedTools is a per-tool grant, or nil when the provider has no
	// vocabulary for one.
	AllowedTools []string
}

// SandboxReadOnly is the mode a provider without a per-tool allowlist is
// confined with.
//
// The spelling is one CLI's vocabulary, which is exactly what this package
// should not know. It is asked for rather than assumed — AcceptsSandbox puts
// it through the provider's own PermissionArgs — so a provider that spells
// confinement differently produces an error naming what it does accept rather
// than a run that was never bounded.
const SandboxReadOnly = "read-only"

// GrantsTools reports whether p can be told, tool by tool, what a run may do.
//
// It discovers the vocabulary by asking, never by switching on the provider's
// ID: a provider with no such vocabulary refuses the request with
// ErrInvalidRequest, which is the signal to fall back to a sandbox mode. Any
// other error is a failure to answer and is returned as one.
func GrantsTools(p agentic.Provider, mode string, tools []string) (bool, error) {
	perm, ok := p.(agentic.Permitter)
	if !ok {
		return false, fmt.Errorf("%s cannot be told what a scripted run may do", p.Descriptor().ID)
	}
	if _, err := perm.PermissionArgs(mode, tools); err == nil {
		return true, nil
	} else if !errors.Is(err, agentic.ErrInvalidRequest) {
		return false, err
	}
	return false, nil
}

// AcceptsSandbox reports whether p accepts mode as a sandbox, by asking it.
func AcceptsSandbox(p agentic.Provider, mode string) error {
	perm, ok := p.(agentic.Permitter)
	if !ok {
		return fmt.Errorf("%s cannot be told what a scripted run may do", p.Descriptor().ID)
	}
	if _, err := perm.PermissionArgs(mode, nil); err != nil {
		return fmt.Errorf("%s does not accept the %q sandbox mode: %w", p.Descriptor().ID, mode, err)
	}
	return nil
}

// ReadOnly returns how to confine a run that only reads.
//
// The narrower grant — the per-tool allowlist — is preferred, and a provider
// without one is bounded by a read-only sandbox, which expresses "reads
// nothing else" at least as tightly as withholding tools from a list does.
//
// A run that writes cannot use this: see the curator, which refuses a sandbox
// fallback outright because the widest sandbox that would let it write covers
// the whole workspace.
func ReadOnly(p agentic.Provider, tools []string) (Bound, error) {
	granted, err := GrantsTools(p, "", tools)
	if err != nil {
		return Bound{}, err
	}
	if granted {
		return Bound{AllowedTools: tools}, nil
	}
	if err := AcceptsSandbox(p, SandboxReadOnly); err != nil {
		return Bound{}, fmt.Errorf("%s has no per-tool allowlist and %w", p.Descriptor().ID, err)
	}
	return Bound{Mode: SandboxReadOnly}, nil
}
