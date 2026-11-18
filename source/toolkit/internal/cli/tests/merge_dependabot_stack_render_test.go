package tests

import (
	"strings"
	"testing"
)

// readMergeDependabot returns the rendered SKILL.md of merge-dependabot with
// its whitespace collapsed, so a sentence wrapped across lines is matched as
// one.
func readMergeDependabot(t *testing.T) string {
	t.Helper()
	apply := renderDefaultStack(t)
	return strings.Join(strings.Fields(readRendered(t, apply, ".claude/skills/merge-dependabot/SKILL.md")), " ")
}

// The skill is prose that nothing else reads, and its reference file is the
// one thing its SKILL.md defers to. A definition the default stack lists but
// that never reaches a consumer fails here rather than in somebody's repo.
func TestTheDefaultStackRendersTheMergeDependabotSkill(t *testing.T) {
	apply := renderDefaultStack(t)

	if !strings.Contains(readRendered(t, apply, ".claude/skills/merge-dependabot/SKILL.md"), "name: merge-dependabot") {
		t.Error("merge-dependabot's SKILL.md arrived without its name")
	}
	if !strings.Contains(readRendered(t, apply, ".claude/skills/merge-dependabot/REFERENCE.md"), "human") {
		t.Error("merge-dependabot's REFERENCE.md did not arrive, so the skill defers to a file a consumer lacks")
	}
}

// A merge the repository's rules refuse is reported, not forced. `--admin`
// merges past a ruleset the run was not meant to override.
func TestMergeDependabotNeverBypassesTheRulesetWithAdmin(t *testing.T) {
	skill := readMergeDependabot(t)

	if !strings.Contains(skill, "- **Never `--admin`**, and never any other way around a ruleset or branch protection.") {
		t.Error("merge-dependabot does not rule out --admin as a hard rule")
	}
	if !strings.Contains(skill, "Never `--admin`, never `--auto`, and never anything else that bypasses a ruleset or branch protection") {
		t.Error("merge-dependabot does not rule out bypassing a ruleset at the merge step")
	}
}

// Approval is a person's act. A run that submitted a review would approve its
// own request for approval.
func TestMergeDependabotNeverSubmitsAReview(t *testing.T) {
	skill := readMergeDependabot(t)

	if !strings.Contains(skill, "**Never submits a review**, of any kind.") {
		t.Error("merge-dependabot does not rule out submitting a review as a hard rule")
	}
	if !strings.Contains(skill, "The run never submits a review") {
		t.Error("merge-dependabot does not say, where it asks for approval, that it never submits one")
	}
}

// The merge authority the git instruction demands is scoped to one author in
// one repository, and checked again before every merge.
func TestMergeDependabotMergesOnlyDependabotAuthoredPullRequests(t *testing.T) {
	skill := readMergeDependabot(t)

	if !strings.Contains(skill, "open pull requests whose author, read over REST, is `dependabot[bot]`") {
		t.Error("merge-dependabot does not scope its merge authority to dependabot-authored pull requests")
	}
	if !strings.Contains(skill, "- **Dependabot pull requests only.**") {
		t.Error("merge-dependabot does not hold dependabot-only as a hard rule")
	}
	if !strings.Contains(skill, "its author is `dependabot[bot]`, its head is the head the run evaluated, and it is still open") {
		t.Error("merge-dependabot does not re-check the author, head and state immediately before a merge")
	}
}

// `/lydite clear` speaks for the head it is posted on and releases a pull
// request lydite referred. Posting it on a pull request a human case applies
// to, before anybody approved, clears it unread.
func TestMergeDependabotClearsOnlyWithNoHumanCaseOrAfterApproval(t *testing.T) {
	skill := readMergeDependabot(t)

	if !strings.Contains(skill, "- **Never posts `/lydite clear`** but on a dependabot pull request that is green with no human case, or approved on its current head.") {
		t.Error("merge-dependabot does not limit /lydite clear to a green pull request with no human case or an approved one")
	}
	if !strings.Contains(skill, "the pull request is green, `referral_pending` is `true`, and no human case applies") {
		t.Error("merge-dependabot does not name the no-human-case condition for clearing")
	}
	if !strings.Contains(skill, "the pull request has been approved, by REFERENCE.md's rule, on its current head") {
		t.Error("merge-dependabot does not name the approval condition for clearing")
	}
}
