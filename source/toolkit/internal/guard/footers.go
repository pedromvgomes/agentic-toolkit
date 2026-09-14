// Package guard holds the decision logic Claude Code hooks call into
// before letting a tool call through. A hook script stays a thin call
// into a tested function here, the same shape ADR 0014 uses for the
// session-start hook and `agtk handoff list`.
package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// Decision is the guard's verdict on one PreToolUse hook invocation.
// Pattern names which footer pattern matched, and is empty when Deny
// is false.
type Decision struct {
	Deny    bool
	Pattern string
}

type footerPattern struct {
	name  string
	match func(text string) bool
}

// footerPatterns are the banned attribution shapes, checked
// case-insensitively against a publishing command's own text and any
// file it points a message at. Extend by appending here.
var footerPatterns = []footerPattern{
	{name: "a Co-Authored-By: line", match: matchLinePrefix("co-authored-by:")},
	{name: "a Claude-Session: line", match: matchLinePrefix("claude-session:")},
	{name: "a claude.ai/code/session link", match: matchSessionLink},
	{name: `a "Generated with" attribution line`, match: matchGeneratedWithLine},
}

func matchLinePrefix(prefix string) func(string) bool {
	prefix = strings.ToLower(prefix)
	return func(text string) bool {
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), prefix) {
				return true
			}
		}
		return false
	}
}

// sessionLinkRe matches an actual link to an assistant session rather than
// prose that merely names the pattern ("no claude.ai/code/session link
// here"): the host, the fixed "code/session_" path segment, and at least
// one non-space character identifying the session, with an optional
// scheme.
var sessionLinkRe = regexp.MustCompile(`(?i)(?:https?://)?claude\.ai/code/session_\S+`)

func matchSessionLink(text string) bool {
	return sessionLinkRe.MatchString(text)
}

// matchGeneratedWithLine denies a line whose own text opens with
// "generated with", once an emoji, markdown marker or other leading
// punctuation is stripped — "🤖 Generated with [Claude Code]" — but
// allows the phrase in the middle of a sentence, such as prose noting a
// file "is generated with" some other tool.
func matchGeneratedWithLine(text string) bool {
	const prefix = "generated with"
	for _, line := range strings.Split(text, "\n") {
		runes := []rune(strings.TrimSpace(line))
		i := 0
		for i < len(runes) && !unicode.IsLetter(runes[i]) {
			i++
		}
		if strings.HasPrefix(strings.ToLower(string(runes[i:])), prefix) {
			return true
		}
	}
	return false
}

type publishingVerb struct {
	name string
	re   *regexp.Regexp
	// requiresMessageArg is set for a verb that only publishes text
	// when a message flag is present — a lightweight git tag carries
	// none, so it never reaches deny.
	requiresMessageArg bool
}

// publishingVerbs are the invocations that write text somewhere other
// commands can read back. Detection is substring/token matching on the
// raw command string, not a shell parser: a command that assembles its
// message from a variable (`git commit -m "$MSG"`) is not seen, and
// that limit is accepted rather than built around.
var publishingVerbs = []publishingVerb{
	{name: "git commit", re: regexp.MustCompile(`(?:^|[;&|\s])git\s+commit\b`)},
	{name: "git tag", re: regexp.MustCompile(`(?:^|[;&|\s])git\s+tag\b`), requiresMessageArg: true},
	{name: "gh pr create/edit/comment/review", re: regexp.MustCompile(`(?:^|[;&|\s])gh\s+pr\s+(?:create|edit|comment|review)\b`)},
	{name: "gh issue create/comment/edit", re: regexp.MustCompile(`(?:^|[;&|\s])gh\s+issue\s+(?:create|comment|edit)\b`)},
	{name: "gh release create/edit", re: regexp.MustCompile(`(?:^|[;&|\s])gh\s+release\s+(?:create|edit)\b`)},
	{name: "gh api", re: regexp.MustCompile(`(?:^|[;&|\s])gh\s+api\b`)},
}

var messageArgRe = regexp.MustCompile(`(?:^|\s)(?:-m|--message|-F|--file)(?:=|\s)`)

// fileArgRe finds a flag that points at a file holding the published
// text: -F/--file on git commit/tag, --body-file on gh, or one of gh
// api's field flags (-f/-F/--field/--raw-field) whose key=value takes
// the form key=@path. The value is either `--flag=path` or `--flag
// path`; referencedFiles below picks the @path out of a key=value pair
// and drops a key=value pair that isn't one.
var fileArgRe = regexp.MustCompile(`(?:-f|-F|--file|--body-file|--field|--raw-field)(?:=(\S+)|\s+(\S+))`)

// atPathRe pulls the path out of gh api's key=@path field syntax.
var atPathRe = regexp.MustCompile(`^[^=]+=@(.+)$`)

func publishes(command string) bool {
	for _, v := range publishingVerbs {
		if !v.re.MatchString(command) {
			continue
		}
		if v.requiresMessageArg && !messageArgRe.MatchString(command) {
			continue
		}
		return true
	}
	return false
}

func referencedFiles(command string) []string {
	var files []string
	for _, m := range fileArgRe.FindAllStringSubmatch(command, -1) {
		val := m[1]
		if val == "" {
			val = m[2]
		}
		val = strings.Trim(val, `"'`)
		if val == "" || val == "-" {
			continue
		}
		if am := atPathRe.FindStringSubmatch(val); am != nil {
			val = am[1]
		} else if strings.Contains(val, "=") {
			// A key=value field (gh api -f/-F name=value) that isn't the
			// key=@path form publishes nothing to a file on disk.
			continue
		}
		files = append(files, val)
	}
	return files
}

type hookPayload struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
	CWD string `json:"cwd"`
}

// DecideFooters reads a Claude Code PreToolUse hook payload and reports
// whether the Bash command it names publishes a banned footer. Malformed
// JSON allows rather than errors: a guard that cannot parse its input
// must not block work it was never asked to judge.
func DecideFooters(payload []byte) Decision {
	var hook hookPayload
	if err := json.Unmarshal(payload, &hook); err != nil {
		return Decision{}
	}
	command := hook.ToolInput.Command
	if hook.ToolName != "Bash" || strings.TrimSpace(command) == "" {
		return Decision{}
	}
	if !publishes(command) {
		return Decision{}
	}

	texts := []string{command}
	for _, rel := range referencedFiles(command) {
		path := rel
		if !filepath.IsAbs(path) {
			path = filepath.Join(hook.CWD, path)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		texts = append(texts, string(content))
	}

	for _, p := range footerPatterns {
		for _, text := range texts {
			if p.match(text) {
				return Decision{Deny: true, Pattern: p.name}
			}
		}
	}
	return Decision{}
}
