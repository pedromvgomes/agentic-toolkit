package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The feature flow is prose that no other test reads, and it is opt-in — so it
// is rendered from its own stack rather than the default one, and a consumer
// extending only this stack has to get a working flow out of it.
func renderFeatureFlowStack(t *testing.T) string {
	t.Helper()

	repo := repoRoot(t)
	source := t.TempDir()
	for _, dir := range []string{"definitions", "stacks"} {
		if err := copyTree(filepath.Join(repo, dir), filepath.Join(source, dir)); err != nil {
			t.Fatalf("copy %s: %v", dir, err)
		}
	}

	apply := t.TempDir()
	cache := t.TempDir()
	_, stderr, err := runCLI(t, apply, "--source", source, "--stack", "feature-flow", "sync", "--cache", cache)
	if err != nil {
		t.Fatalf("sync --source: %v\nstderr:\n%s", err, stderr)
	}
	return apply
}

// The loop drives panel-code-review rather than repeating it. A rendered copy
// carrying its own roster or severity ladder would be a second answer to
// questions the manifest and the binary already answer (ADR 0011).
func TestReviewImplementationDrivesTheReviewSkillItWraps(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/review-implementation/SKILL.md"))
	if err != nil {
		t.Fatalf("review-implementation did not reach the consumer: %v", err)
	}
	skill := string(body)
	if !strings.Contains(skill, "panel-code-review") {
		t.Fatalf("the loop does not name the skill it drives:\n%s", skill)
	}
	if _, err := os.Stat(filepath.Join(apply, ".claude/skills/panel-code-review/SKILL.md")); err != nil {
		t.Errorf("review-implementation drives panel-code-review, which never rendered: %v", err)
	}
}

// A run that could not look and a run that found nothing both report zero
// defects, and only one of them means the branch is clean. A loop that exits
// on the count alone reports a failed review as a passing one.
func TestReviewImplementationDoesNotReadNoVerdictAsClean(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/review-implementation/SKILL.md"))
	if err != nil {
		t.Fatalf("review-implementation did not reach the consumer: %v", err)
	}
	skill := string(body)
	if !strings.Contains(skill, "could not look") {
		t.Errorf("the loop does not separate a failed review from a clean one:\n%s", skill)
	}
	if !strings.Contains(skill, "No verdict") {
		t.Error("the loop has no exit for a run that reached no verdict")
	}
}

// Stalled and capped are failures to converge. A caller reading either as
// good enough opens a pull request carrying the defects the loop just found
// and reported.
func TestReviewImplementationForbidsAPullRequestWhenItDoesNotConverge(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/review-implementation/SKILL.md"))
	if err != nil {
		t.Fatalf("review-implementation did not reach the consumer: %v", err)
	}
	skill := string(body)
	if !strings.Contains(skill, "Do not open a pull request") {
		t.Errorf("a non-converging loop does not withhold the pull request:\n%s", skill)
	}
	if !strings.Contains(skill, "Stalled") {
		t.Error("the loop spends its whole budget to re-learn that fixes are not landing")
	}
}

// GREEN is a remark, and RED and AMBER are both defects. A loop that gated on
// GREEN would never terminate on a change carrying one, and one that ignored
// AMBER would ship a defect the review manifest's own approval floor blocks.
func TestReviewImplementationGatesOnDefectsAndNotOnRemarks(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/review-implementation/SKILL.md"))
	if err != nil {
		t.Fatalf("review-implementation did not reach the consumer: %v", err)
	}
	skill := string(body)
	if !strings.Contains(skill, "GREEN is a remark") {
		t.Errorf("the loop does not say that a remark never keeps it running:\n%s", skill)
	}
	if !strings.Contains(skill, "RED and AMBER") {
		t.Error("the loop does not say which severities it treats as defects")
	}
}

// The loop reviews the working tree. panel-code-review posts when its target
// is a pull request, so a pass that resolved to one would publish a review on
// every iteration.
func TestReviewImplementationPinsTheLocalTarget(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/review-implementation/SKILL.md"))
	if err != nil {
		t.Fatalf("review-implementation did not reach the consumer: %v", err)
	}
	skill := string(body)
	if !strings.Contains(skill, "local target") {
		t.Errorf("the loop does not pin its target, so an open PR turns each pass into a posted review:\n%s", skill)
	}
	if !strings.Contains(skill, "It never reviews a pull request") {
		t.Error("the loop does not rule out reviewing a pull request")
	}
}
