package review

import (
	"fmt"
	"sort"
	"strings"
)

// Signal is a property of a change that agtk detects itself.
//
// The vocabulary is closed and ships with the binary. Detecting one is
// language knowledge, and language knowledge has to be tested somewhere other
// than a consumer's YAML: a repo that wrote its own `sync\.Mutex` pattern gets
// nothing the day it adds a second language, and nobody goes back to update
// it. A repo's own escape hatch is `touches`, which is honest about being
// path-only.
type Signal string

const (
	// SignalAuth is authentication, authorization, session, token or
	// permission logic, including the middleware chains that gate requests.
	SignalAuth Signal = "auth"
	// SignalMigrations is schema migration: DDL, changesets, and the
	// migration directories the ecosystems agree on.
	SignalMigrations Signal = "migrations"
	// SignalSharedKernel is code under a directory many other modules import.
	SignalSharedKernel Signal = "shared-kernel"
	// SignalPublicAPI is a wire-visible contract: exported signatures, REST,
	// gRPC or GraphQL schemas, serialized formats.
	SignalPublicAPI Signal = "public-api"
	// SignalMessageConsumers is code that runs with no request in front of
	// it: queue and topic handlers, event consumers, schedulers.
	SignalMessageConsumers Signal = "message-consumers"
	// SignalCICD is the pipeline itself: workflows, release and publish
	// scripts, the images they build on.
	SignalCICD Signal = "ci-cd"
	// SignalIaC is infrastructure as code.
	SignalIaC Signal = "iac"
	// SignalConcurrency is new or changed mutexes, channels, transactions,
	// atomics and optimistic-lock version fields.
	SignalConcurrency Signal = "concurrency"
	// SignalSensitiveData is PII, payment and billing code, and logging near
	// either.
	SignalSensitiveData Signal = "sensitive-data"
	// SignalCrypto is key material, security hashing, TLS configuration and
	// random-token generation.
	SignalCrypto Signal = "crypto"
	// SignalFeatureFlags is flag definitions, default flips, and the removal
	// of a guard.
	SignalFeatureFlags Signal = "feature-flags"
	// SignalFixRevert is a change to lines that trace back to a revert or a
	// security repair — the change may be undoing it.
	SignalFixRevert Signal = "fix-revert"
	// SignalBugfixLines is a change to lines that trace back to a routine bug
	// fix. Separate from SignalFixRevert because a repo using Conventional
	// Commits spells a third of its history `fix(scope):`, so the two together
	// would name most lines in the tree and say nothing about any of them.
	SignalBugfixLines Signal = "bugfix-lines"
)

// Signals are the vocabulary, in the order `agtk code-review signals` lists
// them.
var Signals = []Signal{
	SignalAuth,
	SignalMigrations,
	SignalSharedKernel,
	SignalPublicAPI,
	SignalMessageConsumers,
	SignalCICD,
	SignalIaC,
	SignalConcurrency,
	SignalSensitiveData,
	SignalCrypto,
	SignalFeatureFlags,
	SignalFixRevert,
	SignalBugfixLines,
}

// signalDescriptions is what `agtk code-review signals` prints beside each
// name: what the signal says about a change, in one line.
var signalDescriptions = map[Signal]string{
	SignalAuth:             "Authentication, authorization, sessions, tokens, permissions, and the middleware that gates requests.",
	SignalMigrations:       "Schema migrations — DDL, changesets, migration directories. A column made NOT NULL without a default is the shape that hurts.",
	SignalSharedKernel:     "Code under a directory many other modules import, where one edit reaches everything downstream.",
	SignalPublicAPI:        "Wire-visible contracts: exported signatures, REST, gRPC and GraphQL schemas, serialized formats.",
	SignalMessageConsumers: "Code that runs with no request in front of it: queue and topic handlers, event consumers, schedulers.",
	SignalCICD:             "The pipeline itself: workflow files, release and publish scripts, the images they build on.",
	SignalIaC:              "Infrastructure as code — Terraform, CloudFormation, Helm charts, Kubernetes manifests.",
	SignalConcurrency:      "New or changed mutexes, channels, transactions, atomics, and optimistic-lock version fields.",
	SignalSensitiveData:    "PII handling, payment and billing code, and logging changes near either.",
	SignalCrypto:           "Key material, hashing done for security, TLS configuration, random-token generation.",
	SignalFeatureFlags:     "Flag definitions, default flips, and the removal of a guard.",
	SignalFixRevert:        "Touched lines that trace back to a revert or a security repair — the change may be undoing it.",
	SignalBugfixLines:      "Touched lines that trace back to a routine bug fix. Common wherever Conventional Commits are used, so it reports breadth rather than danger.",
}

// Description is the one-line account of what s says about a change.
func (s Signal) Description() string { return signalDescriptions[s] }

// Known reports whether s is part of the vocabulary.
func (s Signal) Known() bool {
	_, ok := signalDescriptions[s]
	return ok
}

// errUnknownSignal names the signal and points at the command that lists the
// vocabulary, rather than inlining twelve names into one line.
func errUnknownSignal(name string) error {
	return fmt.Errorf("%q is not a signal; `agtk code-review signals` lists the %d that are", name, len(Signals))
}

// SignalSet is the signals one change carries, together with the ones that
// could not be determined.
//
// Undetermined is tracked rather than collapsed into absent. A signal that
// could not be computed — the blame budget for fix-revert ran out, say — is
// not a signal the change does not carry, and a rule that reads it as absent
// would leave a repo believing it has a protection it does not have.
type SignalSet struct {
	present      map[Signal]bool
	undetermined map[Signal]string
}

// NewSignalSet builds an empty set.
func NewSignalSet() *SignalSet {
	return &SignalSet{present: map[Signal]bool{}, undetermined: map[Signal]string{}}
}

// Add records that the change carries s.
func (s *SignalSet) Add(sig Signal) { s.present[sig] = true }

// MarkUndetermined records that s could not be computed, and why. A signal
// already found present stays present: evidence that arrived beats a budget
// that ran out.
func (s *SignalSet) MarkUndetermined(sig Signal, reason string) {
	if s.present[sig] {
		return
	}
	s.undetermined[sig] = reason
}

// Has reports whether the change carries sig, and whether that answer is
// known at all. A caller that ignores ok reads undetermined as absent, which
// is the one reading this type exists to prevent.
func (s *SignalSet) Has(sig Signal) (has, ok bool) {
	if s.present[sig] {
		return true, true
	}
	if _, undet := s.undetermined[sig]; undet {
		return false, false
	}
	return false, true
}

// Undetermined returns why sig could not be computed, or "" when it could.
func (s *SignalSet) Undetermined(sig Signal) string { return s.undetermined[sig] }

// Present lists the signals the change carries, in vocabulary order.
func (s *SignalSet) Present() []Signal {
	out := make([]Signal, 0, len(s.present))
	for _, sig := range Signals {
		if s.present[sig] {
			out = append(out, sig)
		}
	}
	return out
}

// String renders the set the way `--explain` reports it.
func (s *SignalSet) String() string {
	parts := make([]string, 0, len(s.present)+len(s.undetermined))
	for _, sig := range s.Present() {
		parts = append(parts, string(sig))
	}
	undet := make([]string, 0, len(s.undetermined))
	for sig := range s.undetermined {
		undet = append(undet, string(sig))
	}
	sort.Strings(undet)
	for _, name := range undet {
		parts = append(parts, name+": undetermined")
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}
