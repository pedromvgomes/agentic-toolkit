package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"

	"github.com/pedromvgomes/agentic-toolkit/internal/guard"
)

func payload(t *testing.T, toolName, command, cwd string) []byte {
	t.Helper()
	body := map[string]any{
		"tool_name": toolName,
		"tool_input": map[string]any{
			"command": command,
		},
		"cwd": cwd,
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}

const coAuthoredFooter = "\n\nCo-Authored-By: Someone <someone@example.com>\n"
const sessionFooter = "\n\nClaude-Session: https://claude.ai/code/session_abc\n"
const urlFooter = "\n\nSee https://claude.ai/code/session_abc for context.\n"
const generatedFooter = "\n\n🤖 Generated with [Claude Code]\n"

var footerCases = []struct {
	name   string
	footer string
}{
	{"co-authored-by", coAuthoredFooter},
	{"claude-session", sessionFooter},
	{"session-url", urlFooter},
	{"generated-with", generatedFooter},
}

var publishingCommands = []struct {
	name    string
	command func(text string) string
}{
	{"git commit -m", func(text string) string { return `git commit -m "message` + text + `"` }},
	{"git tag -m", func(text string) string { return `git tag -m "message` + text + `" v1.0.0` }},
	{"gh pr create", func(text string) string { return `gh pr create --title "t" --body "body` + text + `"` }},
	{"gh pr edit", func(text string) string { return `gh pr edit 1 --body "body` + text + `"` }},
	{"gh pr comment", func(text string) string { return `gh pr comment 1 --body "body` + text + `"` }},
	{"gh pr review", func(text string) string { return `gh pr review 1 --body "body` + text + `"` }},
	{"gh issue create", func(text string) string { return `gh issue create --title "t" --body "body` + text + `"` }},
	{"gh issue comment", func(text string) string { return `gh issue comment 1 --body "body` + text + `"` }},
	{"gh issue edit", func(text string) string { return `gh issue edit 1 --body "body` + text + `"` }},
	{"gh release create", func(text string) string { return `gh release create v1.0.0 --notes "body` + text + `"` }},
	{"gh release edit", func(text string) string { return `gh release edit v1.0.0 --notes "body` + text + `"` }},
	{"gh api", func(text string) string { return `gh api repos/o/r/issues -f body=body` + text }},
}

func TestDecideFootersDeniesEachVerbWithEachPattern(t *testing.T) {
	for _, verb := range publishingCommands {
		for _, fc := range footerCases {
			t.Run(verb.name+"/"+fc.name, func(t *testing.T) {
				cmd := verb.command(fc.footer)
				d := guard.DecideFooters(payload(t, "Bash", cmd, "/tmp"))
				if !d.Deny {
					t.Fatalf("command %q: want deny, got allow", cmd)
				}
			})
		}
	}
}

func TestDecideFootersAllowsCleanText(t *testing.T) {
	for _, verb := range publishingCommands {
		t.Run(verb.name, func(t *testing.T) {
			cmd := verb.command("")
			d := guard.DecideFooters(payload(t, "Bash", cmd, "/tmp"))
			if d.Deny {
				t.Fatalf("command %q: want allow, got deny on %s", cmd, d.Pattern)
			}
		})
	}
}

func TestDecideFootersAllowsNonPublishingMentionOfAPattern(t *testing.T) {
	cases := []string{
		`git grep Claude-Session`,
		`rg "Co-Authored-By"`,
		`git log --grep="claude.ai/code/session"`,
		`echo "Generated with something"`,
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			d := guard.DecideFooters(payload(t, "Bash", cmd, "/tmp"))
			if d.Deny {
				t.Fatalf("command %q: want allow, got deny on %s", cmd, d.Pattern)
			}
		})
	}
}

func TestDecideFootersDeniesFooterInCommitMessageFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "msg.txt"), []byte("subject\n\nCo-Authored-By: Someone <s@example.com>\n"), 0o644); err != nil {
		t.Fatalf("write msg file: %v", err)
	}
	d := guard.DecideFooters(payload(t, "Bash", "git commit -F msg.txt", dir))
	if !d.Deny {
		t.Fatalf("want deny, got allow")
	}
}

func TestDecideFootersDeniesFooterInGhBodyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "body.md"), []byte("summary\n\nGenerated with [Claude Code]\n"), 0o644); err != nil {
		t.Fatalf("write body file: %v", err)
	}
	d := guard.DecideFooters(payload(t, "Bash", "gh pr create --title t --body-file body.md", dir))
	if !d.Deny {
		t.Fatalf("want deny, got allow")
	}
}

func TestDecideFootersAllowsGeneratedWithProse(t *testing.T) {
	cases := []string{
		`git commit -m "docs: CONFIG-SCHEMA.md is generated with make generate"`,
		`gh pr create --title t --body "this file is generated with schemagen"`,
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			d := guard.DecideFooters(payload(t, "Bash", cmd, "/tmp"))
			if d.Deny {
				t.Fatalf("command %q: want allow, got deny on %s", cmd, d.Pattern)
			}
		})
	}
}

func TestDecideFootersDeniesGeneratedWithLine(t *testing.T) {
	cases := []string{
		`git commit -m "summary` + "\n\n" + `🤖 Generated with [Claude Code](https://claude.com/claude-code)"`,
		`git commit -m "summary` + "\n\n" + `Generated with Claude Code"`,
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			d := guard.DecideFooters(payload(t, "Bash", cmd, "/tmp"))
			if !d.Deny {
				t.Fatalf("command %q: want deny, got allow", cmd)
			}
		})
	}
}

func TestDecideFootersDeniesGhAPIFieldFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "reply.md"), []byte("thanks\n\nCo-Authored-By: Someone <s@example.com>\n"), 0o644); err != nil {
		t.Fatalf("write reply file: %v", err)
	}
	d := guard.DecideFooters(payload(t, "Bash", `gh api repos/o/r/pulls/1/comments -F body=@reply.md`, dir))
	if !d.Deny {
		t.Fatalf("want deny, got allow")
	}
}

func TestDecideFootersAllowsGhAPIFieldFileClean(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "reply.md"), []byte("thanks for the review\n"), 0o644); err != nil {
		t.Fatalf("write reply file: %v", err)
	}
	d := guard.DecideFooters(payload(t, "Bash", `gh api repos/o/r/pulls/1/comments -F body=@reply.md`, dir))
	if d.Deny {
		t.Fatalf("want allow, got deny on %s", d.Pattern)
	}
}

func TestDecideFootersAllowsNonBashTool(t *testing.T) {
	d := guard.DecideFooters(payload(t, "Write", `git commit -m "x`+coAuthoredFooter+`"`, "/tmp"))
	if d.Deny {
		t.Fatalf("non-Bash tool should always allow, got deny on %s", d.Pattern)
	}
}

func TestDecideFootersAllowsMalformedJSON(t *testing.T) {
	d := guard.DecideFooters([]byte(`{not json`))
	if d.Deny {
		t.Fatalf("malformed payload should allow, got deny on %s", d.Pattern)
	}
}

func TestDecideFootersAllowsSessionLinkProse(t *testing.T) {
	cmd := `git commit -m "docs: mention there is no claude.ai/code/session link here"`
	d := guard.DecideFooters(payload(t, "Bash", cmd, "/tmp"))
	if d.Deny {
		t.Fatalf("command %q: want allow, got deny on %s", cmd, d.Pattern)
	}
}

func TestDecideFootersDeniesSessionLink(t *testing.T) {
	cases := []string{
		`git commit -m "see https://claude.ai/code/session_01ABC for context"`,
		`git commit -m "see claude.ai/code/session_01ABC for context"`,
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			d := guard.DecideFooters(payload(t, "Bash", cmd, "/tmp"))
			if !d.Deny {
				t.Fatalf("command %q: want deny, got allow", cmd)
			}
		})
	}
}

func TestDecideFootersSeesGitCommitPastGlobalOptions(t *testing.T) {
	commands := []struct {
		name    string
		command func(text string) string
	}{
		{"-C", func(text string) string { return `git -C repo commit -m "message` + text + `"` }},
		{"-c", func(text string) string { return `git -c user.name=x commit -m "message` + text + `"` }},
		{"--no-pager", func(text string) string { return `git --no-pager commit -m "message` + text + `"` }},
		{"--git-dir=", func(text string) string { return `git --git-dir=.git commit -m "message` + text + `"` }},
		{"--git-dir", func(text string) string { return `git --git-dir .git commit -m "message` + text + `"` }},
		{"after cd", func(text string) string { return `cd x && git commit -m "message` + text + `"` }},
	}
	for _, c := range commands {
		t.Run(c.name+"/footer", func(t *testing.T) {
			cmd := c.command(coAuthoredFooter)
			if d := guard.DecideFooters(payload(t, "Bash", cmd, "/tmp")); !d.Deny {
				t.Fatalf("command %q: want deny, got allow", cmd)
			}
		})
		t.Run(c.name+"/clean", func(t *testing.T) {
			cmd := c.command("")
			if d := guard.DecideFooters(payload(t, "Bash", cmd, "/tmp")); d.Deny {
				t.Fatalf("command %q: want allow, got deny on %s", cmd, d.Pattern)
			}
		})
	}
}

func TestDecideFootersReadsFileNamesContainingSpaces(t *testing.T) {
	cases := []struct {
		command string
		file    string
	}{
		{`git commit -F "release notes.md"`, "release notes.md"},
		{`git commit -F release\ notes.md`, "release notes.md"},
		{`gh pr create --title t --body-file 'pr body.md'`, "pr body.md"},
		{`gh release create v1 --notes-file "notes file.md"`, "notes file.md"},
		{`gh api repos/o/r/pulls/1/comments -F 'body=@my reply.md'`, "my reply.md"},
		{`git -C sub commit -F msg.txt`, filepath.Join("sub", "msg.txt")},
	}
	for _, c := range cases {
		t.Run(c.command+"/footer", func(t *testing.T) {
			dir := writeFile(t, c.file, "subject"+coAuthoredFooter)
			if d := guard.DecideFooters(payload(t, "Bash", c.command, dir)); !d.Deny {
				t.Fatalf("command %q: want deny, got allow", c.command)
			}
		})
		t.Run(c.command+"/clean", func(t *testing.T) {
			dir := writeFile(t, c.file, "subject\n\nbody\n")
			if d := guard.DecideFooters(payload(t, "Bash", c.command, dir)); d.Deny {
				t.Fatalf("command %q: want allow, got deny on %s", c.command, d.Pattern)
			}
		})
	}
}

func TestDecideFootersJudgesUnparseableCommandOnItsText(t *testing.T) {
	cases := []struct {
		command string
		deny    bool
	}{
		{"git commit -m \"x" + coAuthoredFooter + "\" && (", true},
		{"git commit -m \"clean message\" && (", false},
		{"echo \"x" + coAuthoredFooter + "\" && (", false},
	}
	for _, c := range cases {
		t.Run(c.command, func(t *testing.T) {
			if _, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(c.command), ""); err == nil {
				t.Fatalf("command %q parses; the case needs a command the parser rejects", c.command)
			}
			if d := guard.DecideFooters(payload(t, "Bash", c.command, "/tmp")); d.Deny != c.deny {
				t.Fatalf("command %q: want deny=%v, got deny=%v", c.command, c.deny, d.Deny)
			}
		})
	}
}

func TestDecideFootersAllowsNonPublishingGitWithGlobalOptions(t *testing.T) {
	cases := []string{
		`git -C repo log --grep "x` + coAuthoredFooter + `"`,
		`git -C repo tag v1 && echo "x` + coAuthoredFooter + `"`,
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			if d := guard.DecideFooters(payload(t, "Bash", cmd, "/tmp")); d.Deny {
				t.Fatalf("command %q: want allow, got deny on %s", cmd, d.Pattern)
			}
		})
	}
}

// writeFile writes content to rel under a fresh temp dir and returns the dir.
func writeFile(t *testing.T, rel, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
	return dir
}
