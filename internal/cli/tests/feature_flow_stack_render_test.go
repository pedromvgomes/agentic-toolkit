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

// Everything that changes the branch has to happen before the pull request
// exists. Documentation written after `gh pr create` lands outside the change
// it describes, and the reviewer that writes it is dispatched, not inlined.
func TestOpenPRDocumentsTheBranchBeforeItOpensAnything(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/open-pr/SKILL.md"))
	if err != nil {
		t.Fatalf("open-pr did not reach the consumer: %v", err)
	}
	skill := string(body)
	if !strings.Contains(skill, "wrap-session-reviewer") {
		t.Fatalf("open-pr documents nothing:\n%s", skill)
	}
	if !strings.Contains(skill, "before the pull request exists") {
		t.Error("open-pr does not pin the ordering, so documentation can land outside the change")
	}
	if _, err := os.Stat(filepath.Join(apply, ".claude/agents/wrap-session-reviewer/AGENT.md")); err != nil {
		t.Errorf("open-pr dispatches wrap-session-reviewer, which never rendered: %v", err)
	}
}

// The memory store's notes/ has exactly one writer (ADR 0003), and a note
// needs an anchor — so a finding with no file to point at cannot become one
// however useful it is, and staging it only buys a rejection later.
func TestOpenPRStagesOnlyAnchorableFindingsAndAuthorsNoNote(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/open-pr/SKILL.md"))
	if err != nil {
		t.Fatalf("open-pr did not reach the consumer: %v", err)
	}
	skill := string(body)
	if !strings.Contains(skill, "candidates/") {
		t.Fatalf("open-pr stages nothing into the store:\n%s", skill)
	}
	if !strings.Contains(skill, "can name a file") {
		t.Error("open-pr does not require a finding to name a file, so it stages notes lint will reject")
	}
	if !strings.Contains(skill, "agtk memory stats") {
		t.Error("open-pr stages without locating the store first, so a repo without one gets an invented path")
	}
	if !strings.Contains(skill, "Never write, edit, stamp or delete a note") {
		t.Error("open-pr does not hold to the single-writer rule")
	}
}

// A pull request title becomes a subject on the default branch under squash
// merge, and no commit message underneath it can correct that.
func TestOpenPRTitlesThePullRequestConventionally(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/open-pr/SKILL.md"))
	if err != nil {
		t.Fatalf("open-pr did not reach the consumer: %v", err)
	}
	skill := string(body)
	if !strings.Contains(skill, "conventional-commit subject") {
		t.Errorf("open-pr does not constrain the title:\n%s", skill)
	}
	if _, err := os.Stat(filepath.Join(apply, "CLAUDE.md")); err != nil {
		t.Errorf("open-pr runs under the repo's git rules, which never rendered: %v", err)
	}
}

// A review run cannot reach approval, and the skill that opens a pull request
// is the last place that boundary should be soft.
func TestOpenPRNeitherApprovesNorMerges(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/open-pr/SKILL.md"))
	if err != nil {
		t.Fatalf("open-pr did not reach the consumer: %v", err)
	}
	if !strings.Contains(string(body), "never approves and never merges") {
		t.Error("open-pr does not rule out approving or merging what it just opened")
	}
}

// open-pr runs on branches that never had a handoff, so the bookkeeping for
// one belongs to whatever read it. A skill that consumed the handoff itself
// would strand a caller that stopped before the review converged.
func TestOpenPRLeavesHandoffBookkeepingToItsCaller(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/open-pr/SKILL.md"))
	if err != nil {
		t.Fatalf("open-pr did not reach the consumer: %v", err)
	}
	if !strings.Contains(string(body), "never marks a handoff consumed") {
		t.Error("open-pr claims handoff bookkeeping that belongs to its caller")
	}
}

// panel-code-review routes a pull request's fixes to pr-review-resolver. A
// stack carrying one without the other offers a fix path that does not exist.
func TestTheReviewSkillsFixPathRendersAlongsideIt(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	if _, err := os.Stat(filepath.Join(apply, ".claude/skills/pr-review-resolver/SKILL.md")); err != nil {
		t.Errorf("panel-code-review routes fixes to pr-review-resolver, which never rendered: %v", err)
	}
}

// The plan-shaped half of the format is optional. A handoff written by hand
// halfway through something carries no task list, no slices and no PR title,
// and a format demanding them serves planning only — which abandons the case
// that needs a handoff most.
func TestWriteHandoffServesAHandoffWithNoPlan(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/write-handoff/SKILL.md"))
	if err != nil {
		t.Fatalf("write-handoff did not reach the consumer: %v", err)
	}
	skill := string(body)
	if !strings.Contains(skill, "optional") {
		t.Fatalf("write-handoff makes no part of the format optional:\n%s", skill)
	}
	if !strings.Contains(skill, "and is still a handoff") {
		t.Error("write-handoff does not say a plan-free handoff is valid, so the ad-hoc case has no writer")
	}
	if _, err := os.Stat(filepath.Join(apply, ".claude/skills/write-handoff/references/handoff-template.md")); err != nil {
		t.Errorf("write-handoff prescribes a template that never rendered: %v", err)
	}
}

// A handoff pointing at work that exists only in the context about to be
// discarded is worse than no handoff: the next session resumes on top of it
// and cannot tell what is missing.
func TestWriteHandoffRefusesToPointAtUnsavedWork(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/write-handoff/SKILL.md"))
	if err != nil {
		t.Fatalf("write-handoff did not reach the consumer: %v", err)
	}
	skill := string(body)
	if !strings.Contains(skill, "No uncommitted or unstaged changes") {
		t.Errorf("write-handoff writes over a dirty worktree:\n%s", skill)
	}
	if !strings.Contains(skill, "are pushed") {
		t.Error("write-handoff does not require the branch to be pushed")
	}
}

// The worktree is a constant; what varies is whether the next session may
// start at all. A handoff written with a pull request open waits for it.
func TestWriteHandoffRecordsAPredecessorRatherThanAWorktree(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/write-handoff/SKILL.md"))
	if err != nil {
		t.Fatalf("write-handoff did not reach the consumer: %v", err)
	}
	skill := string(body)
	if !strings.Contains(skill, "predecessor") {
		t.Fatalf("write-handoff records no predecessor, so a gated slice can start early:\n%s", skill)
	}
	if !strings.Contains(skill, "continues in **this** worktree") {
		t.Error("write-handoff leaves the resume location open, which it no longer is")
	}
}

// handoff/ is a fact about how somebody works, not about the project. The
// common dir is what makes one write cover every worktree of a bare repo.
func TestWriteHandoffExcludesItsFolderWithoutTouchingGitignore(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/write-handoff/SKILL.md"))
	if err != nil {
		t.Fatalf("write-handoff did not reach the consumer: %v", err)
	}
	skill := string(body)
	if !strings.Contains(skill, "--git-common-dir") {
		t.Errorf("write-handoff excludes per worktree, so a bare repo needs the line in each:\n%s", skill)
	}
	if !strings.Contains(skill, "info/exclude") {
		t.Error("write-handoff does not use the checkout-local exclude file")
	}
	if !strings.Contains(skill, "never edit the consumer's `.gitignore`") {
		t.Error("write-handoff may write into a consumer's committed ignore rules")
	}
}

// The hook finds the handoff and says what to invoke. A continuation prompt
// to paste as well would be a second way in, and the two would drift.
func TestWriteHandoffHandsOffThroughTheHookAlone(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/write-handoff/SKILL.md"))
	if err != nil {
		t.Fatalf("write-handoff did not reach the consumer: %v", err)
	}
	skill := string(body)
	if !strings.Contains(skill, "/clear") {
		t.Errorf("write-handoff does not name the step the user performs by hand:\n%s", skill)
	}
	if !strings.Contains(skill, "Do not paste a continuation prompt") {
		t.Error("write-handoff offers a second way in alongside the hook")
	}
}

// The hook is the only thing that reaches a session started after /clear, so
// it has to arrive under an event and a matcher Claude Code actually fires.
// Under any other matcher the injection is not rejected, it simply never runs.
func TestTheHandoffHookFiresOnAFreshSessionAndNamesTheSkill(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/settings.json"))
	if err != nil {
		t.Fatalf("settings did not reach the consumer: %v", err)
	}
	settings := string(body)
	if !strings.Contains(settings, "SessionStart") {
		t.Fatalf("settings.json carries no SessionStart hook:\n%s", settings)
	}
	if !strings.Contains(settings, "startup|clear") {
		t.Error("the handoff hook does not match the two ways a session starts with no memory of the one that wrote the handoff")
	}
	if !strings.Contains(settings, "implement-handoff") {
		t.Error("the hook injects no instruction naming the skill that consumes a handoff")
	}
	if _, err := os.Stat(filepath.Join(apply, ".claude/skills/implement-handoff/SKILL.md")); err != nil {
		t.Errorf("the hook names implement-handoff, which never rendered: %v", err)
	}
}

// A consumed handoff moves to handoff/done/. The hook globs depth one, so a
// hook that recursed would keep pointing every fresh session at work that has
// already shipped.
func TestTheHandoffHookIgnoresConsumedHandoffs(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/settings.json"))
	if err != nil {
		t.Fatalf("settings did not reach the consumer: %v", err)
	}
	if !strings.Contains(string(body), "handoff/*.md") {
		t.Errorf("the hook does not glob at depth one, so handoff/done/ is not consumed:\n%s", body)
	}
}

// Work gated behind a pull request must not start before it merges, and the
// step that moves the branch changes where the user is standing.
func TestImplementHandoffHonoursThePredecessorBeforeItStarts(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/implement-handoff/SKILL.md"))
	if err != nil {
		t.Fatalf("implement-handoff did not reach the consumer: %v", err)
	}
	skill := string(body)
	if !strings.Contains(skill, "merged into the base branch") {
		t.Fatalf("implement-handoff starts gated work without checking the gate:\n%s", skill)
	}
	if !strings.Contains(skill, "wait\nfor a yes") && !strings.Contains(skill, "wait for a yes") {
		t.Error("implement-handoff moves the branch without asking")
	}
}

// Two implementers write into one working tree, and the second reads a tree
// the first is still changing — after which neither diff can be reviewed on
// its own, which is the property that makes each reviewable at all.
func TestImplementHandoffRunsOneImplementerAtATime(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/implement-handoff/SKILL.md"))
	if err != nil {
		t.Fatalf("implement-handoff did not reach the consumer: %v", err)
	}
	skill := string(body)
	if !strings.Contains(skill, "Never two at once") {
		t.Errorf("implement-handoff does not rule out concurrent implementers:\n%s", skill)
	}
	if _, err := os.Stat(filepath.Join(apply, ".claude/agents/task-implementer/AGENT.md")); err != nil {
		t.Errorf("implement-handoff dispatches task-implementer, which never rendered: %v", err)
	}
}

// The implementer's transcript never reaches the coordinator, so the report is
// a claim and the diff is the evidence. A coordinator acting on the claim has
// nothing the arrangement was built to give it.
func TestImplementHandoffVerifiesTheDiffRatherThanTheReport(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/implement-handoff/SKILL.md"))
	if err != nil {
		t.Fatalf("implement-handoff did not reach the consumer: %v", err)
	}
	skill := string(body)
	if !strings.Contains(skill, "git diff") {
		t.Errorf("implement-handoff commits on a report it never checked:\n%s", skill)
	}
	if !strings.Contains(skill, "run the verification command yourself") {
		t.Error("implement-handoff trusts the implementer's claim that verification passed")
	}
}

// A handoff marked consumed on a run that never opened a pull request strands
// the work: nothing points at it, and the next session starts from scratch.
func TestImplementHandoffKeepsTheHandoffUntilTheWorkLands(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/skills/implement-handoff/SKILL.md"))
	if err != nil {
		t.Fatalf("implement-handoff did not reach the consumer: %v", err)
	}
	skill := string(body)
	if !strings.Contains(skill, "handoff/done/") {
		t.Fatalf("implement-handoff never consumes a handoff, so the hook points at it forever:\n%s", skill)
	}
	if !strings.Contains(skill, "never marks a handoff consumed on a run that did not reach a pull request") {
		t.Error("implement-handoff may consume a handoff whose work never landed")
	}
}

// Nesting is allowed three layers deep, so nothing about the platform stops an
// implementer spawning another. Denying it the tool is what does.
func TestTheImplementerCannotSpawnAnother(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/agents/task-implementer/AGENT.md"))
	if err != nil {
		t.Fatalf("task-implementer did not reach the consumer: %v", err)
	}
	agent := string(body)
	if !strings.Contains(agent, "disallowed-tools:") {
		t.Fatalf("task-implementer's denied tools did not render under the key Claude Code reads:\n%s", agent)
	}
	if !strings.Contains(agent, "Agent") {
		t.Error("task-implementer is not denied the Agent tool, so one implementer can start another")
	}
	if strings.Contains(agent, "\ntools: [Read, Write, Edit, MultiEdit, Bash, Grep, Glob, Agent") {
		t.Error("task-implementer's allowlist grants back the tool its denylist removes")
	}
}

// A task that commits itself removes the review it exists to be given.
func TestTheImplementerDoesNotCommitItsOwnWork(t *testing.T) {
	apply := renderFeatureFlowStack(t)

	body, err := os.ReadFile(filepath.Join(apply, ".claude/agents/task-implementer/AGENT.md"))
	if err != nil {
		t.Fatalf("task-implementer did not reach the consumer: %v", err)
	}
	agent := string(body)
	if !strings.Contains(agent, "Do not commit") {
		t.Errorf("task-implementer commits its own work, so the coordinator reviews nothing:\n%s", agent)
	}
	if !strings.Contains(agent, "STATUS:") {
		t.Error("task-implementer returns no structured report, so the coordinator parses prose")
	}
}
