package review

// This is the only file in the package that imports the driver, and it
// constructs nothing: it type-asserts a provider's interfaces and calls its
// argument builders. Every other part of code-review — parsing a manifest,
// profiling a change, detecting signals, choosing a panel — is reachable
// without a model, which is the property ADR 0002 makes for the memory
// commands and the reason `grep -rl agentic-driver internal/review` returns
// one name.

import (
	"fmt"

	agentic "github.com/pedromvgomes/agentic-driver"

	"github.com/pedromvgomes/agentic-toolkit/internal/provider"
)

// ReadOnlyTools is the grant a review run asks for.
//
// Reviewers are read-only by construction rather than by instruction. They
// read the change and report findings; nothing in a review run edits, and the
// grant says so in the argv rather than in a prompt a model may reason its way
// around. It is not configurable, because a manifest that could widen it is a
// manifest that could hand a run driven by someone else's diff a licence over
// the repo.
var ReadOnlyTools = []string{"Read", "Grep", "Glob"}

// CheckCapabilities asserts that every run this manifest describes could
// actually be made, before any process starts.
//
// A capability that is absent is absent from the provider's type, so the
// answer is available from a manifest alone. The failures it closes are silent
// in the direction that looks like success: a provider that cannot be
// schema-constrained answers the prompt in competent prose and nothing in the
// reply says the shape was never applied, and a provider handed a confinement
// it cannot express refuses every run at spawn time, which reads as an outage
// rather than as a manifest to fix.
func CheckCapabilities(filePath string, m *Manifest) error {
	for _, name := range sortedMapKeys(m.Reviewers) {
		runner := m.Reviewers[name]
		if err := checkRunner(filePath, "reviewers."+name, runner); err != nil {
			return err
		}
	}
	if m.Judge != nil {
		if err := checkRunner(filePath, "judge", *m.Judge); err != nil {
			return err
		}
	}
	if m.Validator != nil {
		if err := checkRunner(filePath, "validator", *m.Validator); err != nil {
			return err
		}
	}
	return nil
}

// checkRunner resolves one runner's provider and asserts what that run needs.
func checkRunner(filePath, field string, r Runner) error {
	p, err := provider.New(r.Provider)
	if err != nil {
		return wrapFieldErr(filePath, field+".provider", ErrUnknownName, err)
	}

	// Every review run carries a schema: findings arrive as a structured
	// payload from the driver rather than as prose parsed afterwards.
	if _, ok := p.(agentic.SchemaConstrainer); !ok {
		return fieldErr(filePath, field+".provider", ErrUnknownName,
			"%s cannot bind an answer to a schema, and every run in a review is schema-constrained", r.Provider)
	}

	if _, err := provider.ReadOnly(p, ReadOnlyTools); err != nil {
		return wrapFieldErr(filePath, field+".provider", ErrUnknownName,
			fmt.Errorf("a review run is read-only and %s cannot be confined to that: %w", r.Provider, err))
	}
	return nil
}
