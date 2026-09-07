package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

// complete is a manifest that parses, used as the base every failure case
// perturbs one field of.
const complete = `
version: 1
reviewers:
  correctness: {provider: claudecode, model: sonnet, prompt: builtin:correctness}
  security:    {provider: claudecode, model: opus,   prompt: builtin:security}
  performance: {provider: claudecode, model: sonnet, prompt: ./prompts/perf.md}
judge:     {provider: claudecode, model: opus,   prompt: builtin:judge}
validator: {provider: claudecode, model: sonnet, prompt: builtin:validator}
panels:
  quick:    {reviewers: [correctness]}
  standard: {reviewers: [correctness, security]}
  deep:     {reviewers: [correctness, security, performance], quorum: 2}
defaults:
  worktree: quick
  pr:       standard
escalate:
  - to: deep
    all:
      - touches: {matches: ["**/auth/**", "**/migrations/**"]}
  - to: deep
    any:
      - signals: {in: [concurrency, crypto, fix-revert]}
  - to: standard
    all:
      - changed_files: {gte: 20}
`

func parse(t *testing.T, src string) (*review.Manifest, error) {
	t.Helper()
	return review.ParseBytes("manifest.yaml", []byte(src))
}

func mustParse(t *testing.T, src string) *review.Manifest {
	t.Helper()
	m, err := parse(t, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return m
}

func TestACompleteManifestParses(t *testing.T) {
	m := mustParse(t, complete)

	if len(m.Reviewers) != 3 {
		t.Errorf("reviewers = %d, want 3", len(m.Reviewers))
	}
	if m.Panels["deep"].EffectiveQuorum() != 2 {
		t.Errorf("deep quorum = %d, want 2", m.Panels["deep"].EffectiveQuorum())
	}
	// A panel that names no quorum runs one instance of each reviewer, and
	// says so through the accessor rather than leaving every caller to treat
	// zero as one.
	if m.Panels["quick"].EffectiveQuorum() != 1 {
		t.Errorf("quick quorum = %d, want 1", m.Panels["quick"].EffectiveQuorum())
	}
	if got := m.Defaults.Default(review.ContextPR); got != "standard" {
		t.Errorf("pr default = %q, want standard", got)
	}
	if len(m.Escalate) != 3 {
		t.Fatalf("escalate = %d rules, want 3", len(m.Escalate))
	}
}

// A rule's combinator is read off the field that carried it, so no caller has
// to know which of the two it was written under.
func TestARuleReportsItsOwnCombinator(t *testing.T) {
	m := mustParse(t, complete)

	if conds, all := m.Escalate[0].Conditions(); !all || len(conds) != 1 {
		t.Errorf("escalate[0] = (%d conds, all=%v), want (1, true)", len(conds), all)
	}
	if conds, all := m.Escalate[1].Conditions(); all || len(conds) != 1 {
		t.Errorf("escalate[1] = (%d conds, all=%v), want (1, false)", len(conds), all)
	}
}

func TestConditionOperandsDecode(t *testing.T) {
	m := mustParse(t, complete)

	globs := m.Escalate[0].All[0]
	if globs.Key != review.KeyTouches || globs.Operator != review.OpMatches {
		t.Errorf("condition = %s: {%s}, want touches: {matches}", globs.Key, globs.Operator)
	}
	if len(globs.Globs) != 2 || globs.Globs[0] != "**/auth/**" {
		t.Errorf("globs = %q", globs.Globs)
	}

	signals := m.Escalate[1].Any[0]
	if len(signals.Signals) != 3 || signals.Signals[2] != review.SignalFixRevert {
		t.Errorf("signals = %v", signals.Signals)
	}

	count := m.Escalate[2].All[0]
	if count.Key != review.KeyChangedFiles || count.Operator != review.OpGTE || count.Number != 20 {
		t.Errorf("count = %s: {%s: %d}", count.Key, count.Operator, count.Number)
	}
}

// A condition renders back the way it was written, so --explain can name the
// rule that fired without the caller reconstructing it from three fields.
func TestAConditionRendersBackAsWritten(t *testing.T) {
	m := mustParse(t, complete)

	for _, want := range []struct{ got, expect string }{
		{m.Escalate[0].All[0].String(), "touches: {matches: [**/auth/**, **/migrations/**]}"},
		{m.Escalate[1].Any[0].String(), "signals: {in: [concurrency, crypto, fix-revert]}"},
		{m.Escalate[2].All[0].String(), "changed_files: {gte: 20}"},
	} {
		if want.got != want.expect {
			t.Errorf("String() = %q, want %q", want.got, want.expect)
		}
	}
}

// Strict decoding: a key this build does not know is a refusal, not an
// ignored field. A typo silently dropped is a reviewer that never runs on a
// repo that believes it declared one.
func TestAnUnknownKeyIsRefused(t *testing.T) {
	src := strings.Replace(complete, "  quick:    {reviewers: [correctness]}",
		"  quick:    {reviewers: [correctness], quorom: 2}", 1)

	err := expectFailure(t, src)
	if !review.IsKind(err, review.ErrUnknownField) {
		t.Errorf("kind = %v, want unknown_field", err)
	}
}

func TestAnUnreadableVersionIsRefused(t *testing.T) {
	err := expectFailure(t, strings.Replace(complete, "version: 1", "version: 2", 1))
	if !review.IsKind(err, review.ErrUnknownVersion) {
		t.Errorf("kind = %v, want unknown_version", err)
	}
}

// Every name a manifest uses has to be a name it declares, and the refusal
// lists what was declared so the fix does not need the file reopened.
func TestNamesMustResolve(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		mentions []string
	}{
		{
			"panel names an undeclared reviewer",
			strings.Replace(complete, "  quick:    {reviewers: [correctness]}", "  quick:    {reviewers: [corectness]}", 1),
			[]string{"corectness", "correctness, performance, security"},
		},
		{
			"default names an undeclared panel",
			strings.Replace(complete, "  pr:       standard", "  pr:       thorough", 1),
			[]string{"thorough", "deep, quick, standard"},
		},
		{
			"escalation raises to an undeclared panel",
			strings.Replace(complete, "  - to: deep\n    any:", "  - to: deepest\n    any:", 1),
			[]string{"deepest", "deep, quick, standard"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := expectFailure(t, tc.src)
			if !review.IsKind(err, review.ErrUnknownName) {
				t.Fatalf("kind = %v, want unknown_name", err)
			}
			for _, want := range tc.mentions {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to mention %q", err, want)
				}
			}
		})
	}
}

// A review that cannot complete is refused when the manifest is read, rather
// than after a panel has been spent.
func TestAManifestThatCannotRunIsRefused(t *testing.T) {
	cases := []struct{ name, src, mentions string }{
		{"no judge", removeLine(complete, "judge:"), "judge"},
		{"no validator", removeLine(complete, "validator:"), "posts always validates"},
		{"no reviewers", removeBlock(complete, "reviewers:", "judge:"), "at least one reviewer"},
		{"panel staffs nobody", strings.Replace(complete, "  quick:    {reviewers: [correctness]}", "  quick:    {reviewers: []}", 1), "cannot staff itself"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := expectFailure(t, tc.src)
			if !review.IsKind(err, review.ErrMissingRequired) {
				t.Fatalf("kind = %v, want missing_required", err)
			}
			if !strings.Contains(err.Error(), tc.mentions) {
				t.Errorf("error = %q, want it to mention %q", err, tc.mentions)
			}
		})
	}
}

func expectFailure(t *testing.T, src string) error {
	t.Helper()
	m, err := review.ParseBytes("manifest.yaml", []byte(src))
	if err == nil {
		t.Fatalf("parsed %#v, want a refusal", m)
	}
	return err
}

// removeLine drops the one line beginning with prefix.
func removeLine(src, prefix string) string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if !strings.HasPrefix(line, prefix) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// removeBlock drops everything from the line starting with from up to the
// line starting with to.
func removeBlock(src, from, to string) string {
	var out []string
	dropping := false
	for _, line := range strings.Split(src, "\n") {
		switch {
		case strings.HasPrefix(line, from):
			dropping = true
		case strings.HasPrefix(line, to):
			dropping = false
		}
		if !dropping {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
