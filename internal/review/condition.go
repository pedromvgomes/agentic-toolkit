package review

import (
	"fmt"
	"sort"
	"strings"
)

// A condition is always `key: {operator: value}`.
//
// There is no bare form. A bare form means the combinator between two clauses
// is inferred — from how they nest, or from a neighbouring list — and two
// invisible combinators in adjacent lines is the defect this grammar exists to
// avoid. Conjunction over a set is an operator with a name (`all_in`);
// conjunction over globs is two members of `all:`.
//
// Operators are words rather than symbols. `>=` opens a YAML folded block
// scalar, so a rule written with one fails on the header option before any
// schema is reached, and no error message produced afterwards can recover it.

// ConditionKey is the property of a change a condition tests.
type ConditionKey string

const (
	// KeyTouches matches globs against the changed paths.
	KeyTouches ConditionKey = "touches"
	// KeySignals tests the signals agtk detected in the change.
	KeySignals ConditionKey = "signals"
	// KeyChangedLines counts changed lines after mechanical exclusions.
	KeyChangedLines ConditionKey = "changed_lines"
	// KeyChangedFiles counts reviewable files after mechanical exclusions.
	KeyChangedFiles ConditionKey = "changed_files"
	// KeyReferencingFiles counts the files referencing the exported symbols
	// the change modifies.
	KeyReferencingFiles ConditionKey = "referencing_files"
)

// Operator is how a condition's value is compared against its key.
type Operator string

const (
	OpMatches    Operator = "matches"
	OpNotMatches Operator = "not_matches"

	OpIn    Operator = "in"
	OpNotIn Operator = "not_in"
	OpAllIn Operator = "all_in"

	OpGT  Operator = "gt"
	OpGTE Operator = "gte"
	OpLT  Operator = "lt"
	OpLTE Operator = "lte"
	OpEq  Operator = "eq"
)

// operandKind is the shape a key's value takes.
type operandKind int

const (
	operandGlobs operandKind = iota
	operandSignals
	operandInt
)

// keySpec is everything the grammar knows about one condition key: what its
// value looks like, which operators apply to it, and how to say what it is.
//
// It is one table because it is read by three things that must not disagree —
// the parser's validation, the error message that lists what was valid, and
// the generated schema documentation. A second list of operators written
// anywhere else is a list that goes stale without failing.
type keySpec struct {
	Operand     operandKind
	Operators   []Operator
	Description string
}

var conditionKeys = map[ConditionKey]keySpec{
	KeyTouches: {
		Operand:     operandGlobs,
		Operators:   []Operator{OpMatches, OpNotMatches},
		Description: "Globs over the changed paths. The path-only escape hatch a repo owns, and honest about being path-only.",
	},
	KeySignals: {
		Operand:     operandSignals,
		Operators:   []Operator{OpIn, OpNotIn, OpAllIn},
		Description: "Names from the built-in signal vocabulary. `in` holds when the change carries any of them; `all_in` when it carries every one.",
	},
	KeyChangedLines: {
		Operand:     operandInt,
		Operators:   []Operator{OpGT, OpGTE, OpLT, OpLTE, OpEq},
		Description: "Lines added plus removed, counted after mechanical exclusions. A pure rename contributes nothing; a rename with edits contributes its edits.",
	},
	KeyChangedFiles: {
		Operand:     operandInt,
		Operators:   []Operator{OpGT, OpGTE, OpLT, OpLTE, OpEq},
		Description: "Reviewable files, counted after mechanical exclusions.",
	},
	KeyReferencingFiles: {
		Operand:     operandInt,
		Operators:   []Operator{OpGT, OpGTE, OpLT, OpLTE, OpEq},
		Description: "Files referencing the exported symbols the change modifies. Unavailable when no extractor knows the change's languages, and a rule reading an unavailable count is refused rather than read as low.",
	},
}

// ConditionKeys are the keys a condition may test, in the order help and
// diagnostics list them.
var ConditionKeys = []ConditionKey{
	KeyTouches,
	KeySignals,
	KeyChangedLines,
	KeyChangedFiles,
	KeyReferencingFiles,
}

// Operators returns the operators valid for k, in the order diagnostics list
// them. An unknown key has none.
func (k ConditionKey) Operators() []Operator {
	return conditionKeys[k].Operators
}

// Description is the one-line account of what k tests.
func (k ConditionKey) Description() string { return conditionKeys[k].Description }

// Known reports whether k is part of the vocabulary.
func (k ConditionKey) Known() bool {
	_, ok := conditionKeys[k]
	return ok
}

// Accepts reports whether op applies to k.
func (k ConditionKey) Accepts(op Operator) bool {
	for _, valid := range k.Operators() {
		if valid == op {
			return true
		}
	}
	return false
}

// Condition is one `key: {operator: value}` clause.
//
// Exactly one of Globs, Signals or Number carries the operand, decided by the
// key. They are separate fields rather than one `any` because the operand's
// shape is settled at parse time, and a clause that reached evaluation still
// needing a type switch on an interface would be a clause the parser never
// really validated.
type Condition struct {
	// Raw renders the clause back as it was written, for diagnostics.
	Raw string

	Key      ConditionKey
	Operator Operator

	// Globs is the operand for KeyTouches.
	Globs []string
	// Signals is the operand for KeySignals.
	Signals []Signal
	// Number is the operand for the counting keys.
	Number int
}

// String renders the clause the way it was written.
func (c Condition) String() string {
	if c.Raw != "" {
		return c.Raw
	}
	return string(c.Key) + ": {" + string(c.Operator) + ": …}"
}

// listKeys renders the vocabulary for an error message.
func listKeys() string {
	names := make([]string, 0, len(ConditionKeys))
	for _, k := range ConditionKeys {
		names = append(names, string(k))
	}
	return strings.Join(names, ", ")
}

// listOperators renders k's operators for an error message.
func listOperators(k ConditionKey) string {
	ops := k.Operators()
	names := make([]string, 0, len(ops))
	for _, op := range ops {
		names = append(names, string(op))
	}
	return strings.Join(names, ", ")
}

// errUnknownKey names the key and the vocabulary it is not in.
func errUnknownKey(key string) error {
	return fmt.Errorf("%q is not a condition key; use one of %s", key, listKeys())
}

// errWrongOperator names the operator, its key, and what that key does accept.
func errWrongOperator(k ConditionKey, op string) error {
	return fmt.Errorf("%q is not an operator for %s; use one of %s", op, k, listOperators(k))
}

// sortedKeys renders a set of keys in a stable order, for a diagnostic that
// has to name what it found rather than what it expected.
func sortedKeys(found []string) string {
	out := append([]string(nil), found...)
	sort.Strings(out)
	quoted := make([]string, 0, len(out))
	for _, k := range out {
		quoted = append(quoted, fmt.Sprintf("%q", k))
	}
	return strings.Join(quoted, ", ")
}
