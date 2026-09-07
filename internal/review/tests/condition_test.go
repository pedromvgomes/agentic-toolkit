package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

// rule wraps one condition in the smallest manifest that reaches it.
func rule(cond string) string {
	return `
version: 1
reviewers:
  correctness: {provider: claudecode, prompt: builtin:correctness}
judge:     {provider: claudecode, prompt: builtin:judge}
validator: {provider: claudecode, prompt: builtin:validator}
panels:
  quick: {reviewers: [correctness]}
  deep:  {reviewers: [correctness]}
defaults: {worktree: quick, pr: quick}
escalate:
  - to: deep
    all:
      - ` + cond + "\n"
}

func TestEveryKeyAndOperatorPairParses(t *testing.T) {
	valid := []string{
		`touches: {matches: ["a/**"]}`,
		`touches: {not_matches: ["a/**"]}`,
		`signals: {in: [auth]}`,
		`signals: {not_in: [auth]}`,
		`signals: {all_in: [auth, crypto]}`,
		`changed_lines: {gt: 1}`,
		`changed_lines: {gte: 1}`,
		`changed_lines: {lt: 1}`,
		`changed_lines: {lte: 1}`,
		`changed_lines: {eq: 1}`,
		`changed_files: {gte: 20}`,
		`referencing_files: {gte: 20}`,
	}
	for _, cond := range valid {
		t.Run(cond, func(t *testing.T) {
			if _, err := review.ParseBytes("manifest.yaml", []byte(rule(cond))); err != nil {
				t.Errorf("parse: %v", err)
			}
		})
	}
}

// The bare form is what the grammar exists to refuse: it means a combinator is
// inferred from the neighbouring list rather than written, and the refusal has
// to say what to write instead.
func TestABareConditionIsRefusedWithTheFormToUse(t *testing.T) {
	err := refuse(t, rule(`touches: ["**/auth/**"]`))

	for _, want := range []string{"not a bare value", "matches, not_matches", "touches: {matches:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

// Two keys under one condition would be an invisible AND. The refusal names
// both, and names where the conjunction actually goes.
func TestTwoKeysInOneConditionIsRefused(t *testing.T) {
	err := refuse(t, rule(`touches: {matches: ["a/**"]}`+"\n        signals: {in: [auth]}"))

	for _, want := range []string{"one `key: {operator: value}`", `"signals", "touches"`, "`all:`"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

// Two operators under one key would be the same invisible AND one level down.
func TestTwoOperatorsUnderOneKeyIsRefused(t *testing.T) {
	err := refuse(t, rule(`touches: {matches: ["a/**"], not_matches: ["b/**"]}`))

	for _, want := range []string{"exactly one operator", `"matches", "not_matches"`, "`all:`"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

func TestAnUnknownKeyNamesTheVocabulary(t *testing.T) {
	err := refuse(t, rule(`depth: {gte: 2}`))

	for _, want := range []string{`"depth" is not a condition key`, "touches, signals, changed_lines, changed_files, referencing_files"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

// An operator valid somewhere else is the likeliest mistake, so the refusal
// lists the ones this key does take rather than the whole operator set.
func TestAnOperatorFromAnotherKeyNamesTheRightOnes(t *testing.T) {
	err := refuse(t, rule(`changed_lines: {matches: ["a/**"]}`))

	for _, want := range []string{`"matches" is not an operator for changed_lines`, "gt, gte, lt, lte, eq"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "not_matches") {
		t.Errorf("error = %q, want it to list only the operators changed_lines takes", err)
	}
}

// The signal vocabulary is closed, and the refusal points at the command that
// lists it rather than inlining twelve names.
func TestAnUnknownSignalIsRefused(t *testing.T) {
	err := refuse(t, rule(`signals: {in: [concurrancy]}`))

	for _, want := range []string{`"concurrancy" is not a signal`, "agtk code-review signals"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

// A count is never negative, and a comparison against one either always holds
// or never does — a rule that reads as a protection and is not one.
func TestANegativeCountIsRefused(t *testing.T) {
	err := refuse(t, rule(`changed_files: {gte: -1}`))

	if !strings.Contains(err.Error(), "a count never is") {
		t.Errorf("error = %q, want it to say a count is never negative", err)
	}
}

// An empty list can never hold, so a rule carrying one is a protection that
// silently never fires.
func TestAnEmptyOperandIsRefused(t *testing.T) {
	err := refuse(t, rule(`signals: {in: []}`))

	if !strings.Contains(err.Error(), "can never hold") {
		t.Errorf("error = %q, want it to say the condition can never hold", err)
	}
}

// This is why the operators are words. `>=` opens a YAML folded block scalar,
// so the document fails on the header option before any schema is consulted —
// there is no error message the grammar could produce at that point, which is
// the whole argument for the spelling.
func TestASymbolOperatorFailsInTheYAMLLayer(t *testing.T) {
	err := refuse(t, rule(`changed_files: {>=: 20}`))

	if !review.IsKind(err, review.ErrYAMLSyntax) {
		t.Fatalf("kind = %v, want yaml_syntax — the point is that YAML rejects it first", err)
	}
	if strings.Contains(err.Error(), "changed_files") {
		t.Errorf("error = %q; YAML failing first is exactly why the grammar cannot explain this one", err)
	}
}

// A rule carries exactly one combinator, named on the rule. Disjunction
// between two conjunctions is two rules.
func TestARuleCarriesExactlyOneCombinator(t *testing.T) {
	both := strings.Replace(rule(`touches: {matches: ["a/**"]}`),
		"    all:\n      - touches: {matches: [\"a/**\"]}",
		"    all:\n      - touches: {matches: [\"a/**\"]}\n    any:\n      - signals: {in: [auth]}", 1)

	err := refuse(t, both)
	if !review.IsKind(err, review.ErrInvalidCondition) {
		t.Fatalf("kind = %v, want invalid_condition", err)
	}
	if !strings.Contains(err.Error(), "exactly one of `all:` or `any:`") {
		t.Errorf("error = %q", err)
	}
}

func TestARuleWithNoConditionsIsRefused(t *testing.T) {
	err := refuse(t, strings.Replace(rule(`touches: {matches: ["a/**"]}`),
		"    all:\n      - touches: {matches: [\"a/**\"]}\n", "", 1))

	if !review.IsKind(err, review.ErrMissingRequired) {
		t.Errorf("kind = %v, want missing_required", err)
	}
}

func refuse(t *testing.T, src string) error {
	t.Helper()
	m, err := review.ParseBytes("manifest.yaml", []byte(src))
	if err == nil {
		t.Fatalf("parsed %#v, want a refusal", m)
	}
	return err
}

// A number too large to hold is refused before the conversion, not inspected
// after it. Past the platform's int a value silently becomes a different
// number — on a 32-bit build `gte: 4294967296` lands on zero and the rule then
// fires on every change, which is the protection-that-is-not-one this grammar
// exists to prevent.
func TestACountTooLargeToHoldIsRefused(t *testing.T) {
	for _, value := range []string{"18446744073709551615", "9223372036854775808"} {
		t.Run(value, func(t *testing.T) {
			err := refuse(t, rule(`changed_files: {gte: `+value+`}`))

			if !strings.Contains(err.Error(), "larger than any count") &&
				!strings.Contains(err.Error(), "outside the range") {
				t.Errorf("error = %q, want it to say the number is out of range", err)
			}
		})
	}
}
