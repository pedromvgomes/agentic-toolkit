package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

// twoRosters is the shape the context condition exists for: one roster per
// provider, the same criterion written twice, and the pull-request copy guarded
// so it cannot fire locally.
//
// Without the guard both rules fire in both contexts and the winner is decided
// by which was written first, because `deeper` is strict and equal cost is not
// a raise — so the cheaper ladder would be unreachable.
const twoRosters = `
version: 1
reviewers:
  unified:     {provider: claudecode, model: sonnet, prompt: builtin:unified}
  security:    {provider: claudecode, model: opus,   prompt: builtin:security}
  unified-x:   {provider: codex, model: sol,   prompt: builtin:unified}
  security-x:  {provider: codex, model: astra, prompt: builtin:security}
judge:     {provider: claudecode, model: opus,   prompt: builtin:judge}
validator: {provider: claudecode, model: sonnet, prompt: builtin:validator}
panels:
  quick:       {reviewers: [unified]}
  deep:        {reviewers: [unified, security], quorum: 2}
  quick-x:     {reviewers: [unified-x]}
  deep-x:      {reviewers: [unified-x, security-x], quorum: 2}
defaults:
  worktree: quick
  pr:       quick-x
escalate:
  - to: deep
    all:
      - signals: {in: [auth]}
      - context: {in: [worktree]}
  - to: deep-x
    all:
      - signals: {in: [auth]}
      - context: {in: [pr]}
`

// The same criterion raises to the roster belonging to the context it fired
// in, which is the whole point of the key.
func TestAContextConditionSendsEachContextToItsOwnRoster(t *testing.T) {
	m := mustParse(t, twoRosters)
	p := profile(1, 10, review.AvailableCount(0), review.SignalAuth)

	for _, tc := range []struct {
		ctx  review.Context
		want string
	}{
		{review.ContextWorktree, "deep"},
		{review.ContextPR, "deep-x"},
	} {
		sel, err := review.Select(m, tc.ctx, p, "")
		if err != nil {
			t.Fatalf("%s: %v", tc.ctx, err)
		}
		if sel.Panel != tc.want {
			t.Errorf("%s escalated to %q, want %q", tc.ctx, sel.Panel, tc.want)
		}
	}
}

// A rule whose context does not match contributes nothing, so a change with no
// other reason to escalate stays on its context's default.
func TestAContextConditionWithholdsTheRuleElsewhere(t *testing.T) {
	m := mustParse(t, twoRosters)
	p := profile(1, 10, review.AvailableCount(0)) // no signals: neither criterion holds

	sel, err := review.Select(m, review.ContextPR, p, "")
	if err != nil {
		t.Fatal(err)
	}
	if sel.Panel != "quick-x" {
		t.Errorf("panel = %q, want the pr default quick-x", sel.Panel)
	}
	if len(sel.Fired) != 0 {
		t.Errorf("fired = %v, want nothing", sel.Fired)
	}
}

// `not_in` is the inverse and reads as "everywhere but here".
func TestAContextConditionAcceptsNotIn(t *testing.T) {
	src := strings.Replace(twoRosters, "      - context: {in: [worktree]}", "      - context: {not_in: [pr]}", 1)
	m := mustParse(t, src)
	p := profile(1, 10, review.AvailableCount(0), review.SignalAuth)

	sel, err := review.Select(m, review.ContextWorktree, p, "")
	if err != nil {
		t.Fatal(err)
	}
	if sel.Panel != "deep" {
		t.Errorf("panel = %q, want deep", sel.Panel)
	}
}

// A context this build does not run in is refused when the manifest is read,
// rather than becoming a rule that silently never fires.
func TestAnUnknownContextIsRefused(t *testing.T) {
	err := refuse(t, strings.Replace(twoRosters, "      - context: {in: [worktree]}", "      - context: {in: [prod]}", 1))

	if !strings.Contains(err.Error(), "prod") {
		t.Errorf("error = %q, want it to name the unknown context", err)
	}
	for _, want := range []string{"worktree", "pr"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to list %q", err, want)
		}
	}
}

// An operator the key does not take is refused. `matches` is for globs.
func TestAContextConditionRefusesAGlobOperator(t *testing.T) {
	err := refuse(t, strings.Replace(twoRosters, "      - context: {in: [worktree]}", "      - context: {matches: [worktree]}", 1))

	if !strings.Contains(err.Error(), "matches") {
		t.Errorf("error = %q, want it to name the rejected operator", err)
	}
}
