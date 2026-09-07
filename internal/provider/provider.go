// Package provider resolves a configured provider name to an agentic-driver
// provider, and works out how a read-only run is confined on it.
//
// It exists so the names `memory.agent` accepts and the names a reviewer's
// `provider:` accepts are one list. Two copies of the switch drift, and the
// drift is silent: a name one subsystem knows and the other does not reads as
// a typo in the manifest rather than as a gap in the toolkit.
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
// A name the driver has no provider for is a gap to fill in the driver, where
// the dialect knowledge is tested, rather than an escape hatch here.
func New(name string) (agentic.Provider, error) {
	switch name {
	case "":
		return nil, ErrUnnamed
	case "claudecode":
		// On PATH, not vendored: agtk runs on a developer's machine against
		// the CLI they are already authenticated with.
		return claudecode.NewOnPath()
	case "codex":
		return codex.New(), nil
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

// sandboxReadOnly is the sandbox mode a provider without a per-tool allowlist
// is confined with.
//
// The spelling is one CLI's vocabulary, which is exactly what this package is
// not supposed to know. It is asked for rather than assumed: ReadOnly puts it
// through the provider's own PermissionArgs and refuses when that comes back
// with a refusal, so a provider that spells confinement differently produces
// an error naming what it does accept instead of a run that was never bounded.
const sandboxReadOnly = "read-only"

// ReadOnly returns how to confine a run that must only read, given the tool
// grant it would like to have.
//
// It discovers the provider's vocabulary by asking, never by switching on the
// provider's ID: the narrower grant — the per-tool allowlist — is offered
// first, and a provider that has no such vocabulary refuses it with
// ErrInvalidRequest, which is the signal to fall back to a sandbox mode.
//
// The failure this closes is silent in both directions. A provider handed an
// allowlist it cannot express refuses every run at spawn time, which looks
// like an outage rather than a misconfiguration; a provider handed a sandbox
// mode it does not know accepts the flag and runs unbounded.
func ReadOnly(p agentic.Provider, tools []string) (Bound, error) {
	perm, ok := p.(agentic.Permitter)
	if !ok {
		return Bound{}, fmt.Errorf("%s cannot be told what a scripted run may do, so a read-only run cannot be bounded on it", p.Descriptor().ID)
	}

	if _, err := perm.PermissionArgs("", tools); err == nil {
		return Bound{AllowedTools: tools}, nil
	} else if !errors.Is(err, agentic.ErrInvalidRequest) {
		return Bound{}, err
	}

	bound := Bound{Mode: sandboxReadOnly}
	if _, err := perm.PermissionArgs(bound.Mode, nil); err != nil {
		return Bound{}, fmt.Errorf("%s has no per-tool allowlist and does not accept the %q sandbox mode: %w", p.Descriptor().ID, sandboxReadOnly, err)
	}
	return bound, nil
}
