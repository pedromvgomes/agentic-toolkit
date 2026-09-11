package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/review"
)

// A rule the context excludes is not applicable, so a clause of it that could
// not be read decides nothing and is not reported.
//
// Without this a repo whose language has no symbol extractor sees every
// pull-request-only rule reported as unreadable on every local review — and a
// repo-written manifest fails the review outright, over a rule that would not
// have applied.
const guardedRule = `
version: 1
reviewers:
  unified: {provider: claudecode, model: sonnet, prompt: builtin:unified}
judge:     {provider: claudecode, model: opus,   prompt: builtin:judge}
validator: {provider: claudecode, model: sonnet, prompt: builtin:validator}
panels:
  quick: {reviewers: [unified]}
  deep:  {reviewers: [unified], quorum: 3}
defaults:
  worktree: quick
  pr:       quick
escalate:
  - to: deep
    all:
      - referencing_files: {gte: 20}
      - context: {in: [pr]}
`

// Only a context guard settles a rule. A clause that merely did not match is
// still a protection the repo asked for, so an unreadable sibling has to refuse
// on the first review rather than on whichever later one happens to touch the
// right paths — a manifest that looks healthy until then is the failure the
// refusal exists to prevent.
const unreadableBesideAGlob = `
version: 1
reviewers:
  unified: {provider: claudecode, model: sonnet, prompt: builtin:unified}
judge:     {provider: claudecode, model: opus,   prompt: builtin:judge}
validator: {provider: claudecode, model: sonnet, prompt: builtin:validator}
panels:
  quick: {reviewers: [unified]}
  deep:  {reviewers: [unified], quorum: 3}
defaults:
  worktree: quick
  pr:       quick
escalate:
  - to: deep
    all:
      - touches: {matches: ["**/pkg/**"]}
      - referencing_files: {gte: 20}
`

func TestAnUnreadableClauseRefusesEvenWhenAnotherClauseDidNotMatch(t *testing.T) {
	m := mustParse(t, unreadableBesideAGlob)
	// The change is outside pkg/, so `touches` is a readable no. The rule
	// still cannot be evaluated, and the repo has to hear about it now.
	p := withPaths(profile(1, 10, review.UnavailableCount("no symbol extractor for unrecognised files")), "cmd/main.zig")

	if _, err := review.Select(m, review.ContextWorktree, p, ""); err == nil {
		t.Fatal("a rule that cannot be evaluated was accepted because another clause did not match")
	}
}

func TestAContextExcludedRuleIsNotReportedUnreadable(t *testing.T) {
	m := mustParse(t, guardedRule)
	p := profile(1, 10, review.UnavailableCount("no symbol extractor for unrecognised files"))

	// A repo-written manifest: an unreadable clause is normally a refusal.
	sel, err := review.Select(m, review.ContextWorktree, p, "")
	if err != nil {
		t.Fatalf("a rule guarded to another context refused a local review: %v", err)
	}
	if len(sel.Skipped) != 0 {
		t.Errorf("skipped = %v, want nothing: the rule does not apply here", sel.Skipped)
	}

	// In the context it does apply to, the unreadable clause still refuses.
	if _, err := review.Select(m, review.ContextPR, p, ""); err == nil {
		t.Error("the rule applies in pr and its clause could not be read, yet the review was allowed")
	} else if !strings.Contains(err.Error(), "referencing_files") {
		t.Errorf("error = %q, want it to name the clause", err)
	}
}
