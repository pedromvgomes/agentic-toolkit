package tests

import (
	"strings"
	"testing"
)

// A candidate whose front matter is not valid YAML cannot be read, so it drops
// out of the backlog. An unquoted colon inside `about:` is enough, which is why
// both writers of candidates are told to quote it.
func TestTheExplorerQuotesAboutAndIsNotToldToRunLint(t *testing.T) {
	apply := renderDefaultStack(t)
	explorer := readRendered(t, apply, ".claude/agents/memory-explorer/AGENT.md")

	if !strings.Contains(explorer, `about: "`) {
		t.Errorf("the explorer's candidate template shows an unquoted about:\n%s", explorer)
	}
	if !strings.Contains(explorer, "Always wrap it in double quotes") {
		t.Error("the explorer is not told to quote about:, so a colon in it makes the candidate unreadable")
	}
	// Only stats, show and candidates are pre-approved for the explorer, so a
	// lint step would prompt on every delegation.
	if strings.Contains(explorer, "agtk memory lint") {
		t.Error("the explorer is told to run agtk memory lint, which is not pre-approved and prompts on every delegation")
	}
}

func TestOpenPRQuotesAboutAndLintsWhatItStaged(t *testing.T) {
	apply := renderDefaultStack(t)
	skill := readRendered(t, apply, ".claude/skills/open-pr/SKILL.md")

	if !strings.Contains(skill, "Always wrap `about` in double quotes") {
		t.Error("open-pr does not tell the session to quote about:, so a colon in it makes the candidate unreadable")
	}
	if !strings.Contains(skill, "agtk memory lint") {
		t.Error("open-pr stages candidates without linting them, so an unreadable one is committed unnoticed")
	}
	if strings.Index(skill, "agtk memory lint") > strings.Index(skill, "## 3 — Commit and push") {
		t.Error("open-pr lints the candidates only after committing them")
	}
}

// A curate run leaves its changes in the working tree; without the hand-off the
// session that ran it never commits them.
func TestMemoryCurateHandsTheStoreChangesToTheSessionToCommit(t *testing.T) {
	apply := renderDefaultStack(t)
	command := readRendered(t, apply, ".claude/commands/memory-curate.md")

	if !strings.Contains(command, "git status --short <root>") {
		t.Errorf("memory-curate does not list the store's changes:\n%s", command)
	}
	if !strings.Contains(command, "agtk memory stats") {
		t.Errorf("memory-curate assumes the store's location instead of reading it:\n%s", command)
	}
	if strings.Contains(command, "git status --short .memory") {
		t.Errorf("memory-curate hardcodes the default store location:\n%s", command)
	}
	if !strings.Contains(command, "under the repo's git rules") {
		t.Error("memory-curate does not hand the commit to the session under the repo's git rules")
	}
}
