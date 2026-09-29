package tests

import (
	"os"
	"os/exec"
	"path/filepath"
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

// The command resolves a relative --files path against the working directory, so
// telling the explorer they are relative to project_root sends a search from a
// subdirectory to the wrong path, and it quietly returns nothing.
func TestTheExplorerIsToldFilesAreRelativeToTheWorkingDirectory(t *testing.T) {
	apply := renderDefaultStack(t)
	explorer := readRendered(t, apply, ".claude/agents/memory-explorer/AGENT.md")

	if strings.Contains(explorer, "(paths relative to `project_root`)") {
		t.Error("the explorer is told --files paths are relative to project_root, but the command resolves them against the working directory")
	}
	if !strings.Contains(explorer, "paths relative to your working directory, or absolute") {
		t.Error("the explorer is not told that --files paths are relative to the working directory, or absolute")
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

// The fold runs before the commit step so the shard lands in the same commit as
// the candidates; an installed binary that predates the subcommand is probed
// first so the skill skips the fold instead of failing.
func TestOpenPRFoldsHitsBeforeTheCommitStep(t *testing.T) {
	apply := renderDefaultStack(t)
	skill := readRendered(t, apply, ".claude/skills/open-pr/SKILL.md")

	const probe = "agtk memory hits fold --help 2>&1 | grep -q -- '--check'"
	const fold = "agtk memory hits fold\n```"
	commit := strings.Index(skill, "## 3 — Commit and push")
	if commit < 0 {
		t.Fatalf("open-pr has no commit step:\n%s", skill)
	}
	probeAt := strings.Index(skill, probe)
	foldAt := strings.Index(skill, fold)
	if probeAt < 0 {
		t.Error("open-pr folds without probing whether the installed agtk knows the subcommand")
	}
	if foldAt < 0 {
		t.Error("open-pr never folds the local hit log into a shard")
	}
	if probeAt > foldAt {
		t.Error("open-pr folds before probing for the subcommand, so an older binary fails the skill")
	}
	if foldAt > commit {
		t.Error("open-pr folds only after the commit step, so the shard misses the memory commit")
	}
	if !strings.Contains(skill, "never commits, so the shard is left for the next step") {
		t.Error("open-pr does not say that the fold leaves the commit to the session")
	}
}

// A fold or a staged candidate that is never committed is lost with the
// container, so the commit step ends on a check that the store has nothing left.
func TestOpenPRChecksTheStoreIsCleanBeforePushing(t *testing.T) {
	apply := renderDefaultStack(t)
	skill := readRendered(t, apply, ".claude/skills/open-pr/SKILL.md")

	commit := strings.Index(skill, "## 3 — Commit and push")
	open := strings.Index(skill, "## 4 — Open it")
	guard := strings.Index(skill, "git status --short -- <root>")
	if commit < 0 || open < 0 {
		t.Fatalf("open-pr lacks its commit or open step:\n%s", skill)
	}
	if guard < commit || guard > open {
		t.Error("open-pr does not check the store for uncommitted files within the commit step")
	}
	if !strings.Contains(skill[commit:open], "before pushing") {
		t.Error("open-pr does not require the leftover store files to be handled before the push")
	}
}

// An older agtk answers `memory hits --help` with the parent `memory` help and
// exits 0, so the probe passes only if it asks for the `fold` help and looks for
// a flag that help alone lists. The probe is run as the skill writes it, against
// a fake agtk on PATH.
func TestOpenPRProbeAcceptsOnlyAnAgtkThatKnowsHitsFold(t *testing.T) {
	apply := renderDefaultStack(t)
	skill := readRendered(t, apply, ".claude/skills/open-pr/SKILL.md")

	intro := strings.Index(skill, "knows the subcommand")
	if intro < 0 {
		t.Fatalf("open-pr does not introduce a probe for the subcommand:\n%s", skill)
	}
	const fence = "```bash\n"
	start := strings.Index(skill[intro:], fence)
	if start < 0 {
		t.Fatalf("open-pr has no probe block after its introduction:\n%s", skill)
	}
	block := skill[intro+start+len(fence):]
	probe := strings.TrimSpace(block[:strings.Index(block, "```")])

	const current = `#!/bin/sh
if [ "$1 $2 $3" = "memory hits fold" ]; then
	printf 'Usage: agtk memory hits fold [flags]\n\n      --check   report without writing\n'
	exit 0
fi
exit 1
`
	const older = `#!/bin/sh
if [ "$1" = "memory" ]; then
	printf 'Usage: agtk memory <command>\n\nCommands:\n  stats\n  show\n  candidates\n'
	exit 0
fi
exit 1
`

	tests := []struct {
		name   string
		script string
		wantOK bool
	}{
		{"an agtk that knows hits fold", current, true},
		{"an agtk that answers with the parent memory help", older, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			if err := os.WriteFile(filepath.Join(bin, "agtk"), []byte(tc.script), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("sh", "-c", probe)
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			out, err := cmd.CombinedOutput()
			if tc.wantOK && err != nil {
				t.Errorf("the probe rejects an agtk that knows hits fold: %v\n%s", err, out)
			}
			if !tc.wantOK && err == nil {
				t.Errorf("the probe accepts an agtk that answers with the parent memory help:\n%s", out)
			}
		})
	}
}

// A curate run always leaves tracked changes, so it only lists them; the
// clean-tree guard belongs to the session that commits.
func TestMemoryCurateHasNoCleanTreeGuard(t *testing.T) {
	apply := renderDefaultStack(t)
	command := readRendered(t, apply, ".claude/commands/memory-curate.md")

	if strings.Contains(command, "git status --short -- <root>") {
		t.Error("memory-curate carries the clean-tree guard, which a run that changed the store can never satisfy")
	}
	if strings.Contains(command, "must print nothing") {
		t.Error("memory-curate demands an empty status, which a run that changed the store can never satisfy")
	}
	if strings.Contains(command, "agtk memory hits") {
		t.Error("memory-curate takes on the hits fold, which belongs to open-pr")
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
