package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
