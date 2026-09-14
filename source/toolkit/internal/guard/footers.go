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

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
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

// word is one resolved argument of a parsed command. known is false for
// a word whose value depends on expansion, which never matches a flag,
// subcommand or path but still occupies its position.
type word struct {
	val   string
	known bool
}

func (w word) is(names ...string) bool {
	if !w.known {
		return false
	}
	for _, n := range names {
		if w.val == n {
			return true
		}
	}
	return false
}

func (w word) hasPrefix(prefix string) (string, bool) {
	if !w.known || !strings.HasPrefix(w.val, prefix) {
		return "", false
	}
	return strings.TrimPrefix(w.val, prefix), true
}

// resolveWord reduces a shell word to its literal value, with quotes and
// escapes removed. A message or path assembled from a variable, command
// substitution or other expansion is not seen: the word resolves as
// unknown.
func resolveWord(w *syntax.Word) word {
	expands := false
	syntax.Walk(w, func(n syntax.Node) bool {
		switch n.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp, *syntax.ProcSubst, *syntax.ExtGlob:
			expands = true
		}
		return !expands
	})
	if expands {
		return word{}
	}
	fields, err := expand.Fields(nil, w)
	if err != nil || len(fields) != 1 {
		return word{}
	}
	return word{val: fields[0], known: true}
}

// gitValuedOptions are git's global options that take the next argument
// as their value, or an attached `=value` in their long form.
var gitValuedOptions = map[string]bool{
	"-C": true, "-c": true,
	"--git-dir": true, "--work-tree": true, "--namespace": true,
	"--exec-path": true, "--config-env": true, "--super-prefix": true,
}

// gitPublishedFiles reports whether a git invocation publishes text and
// the files it reads that text from. Relative paths resolve against the
// cumulative -C directory, itself rooted at cwd.
func gitPublishedFiles(args []word, cwd string) (bool, []string) {
	dir := cwd
	i := 1
	for ; i < len(args); i++ {
		a := args[i]
		if !a.known || !strings.HasPrefix(a.val, "-") {
			break
		}
		if a.is("-C") && i+1 < len(args) {
			if c := args[i+1]; c.known {
				dir = joinPath(dir, c.val)
			}
		}
		if gitValuedOptions[a.val] {
			i++
		}
	}
	if i >= len(args) {
		return false, nil
	}
	rest := args[i+1:]
	switch {
	case args[i].is("commit"):
	case args[i].is("tag"):
		if !tagCarriesMessage(rest) {
			return false, nil
		}
	default:
		return false, nil
	}
	var files []string
	for j, a := range rest {
		if p, ok := valueOf(rest, j, "--file"); ok {
			files = append(files, p)
		} else if a.is("-F") && j+1 < len(rest) && rest[j+1].known {
			files = append(files, rest[j+1].val)
		} else if p, ok := a.hasPrefix("-F"); ok && p != "" {
			files = append(files, p)
		}
	}
	return true, resolvePaths(files, dir)
}

// tagCarriesMessage separates an annotated tag, whose message is published
// text, from a lightweight tag, which carries none.
func tagCarriesMessage(args []word) bool {
	for _, a := range args {
		if !a.known {
			continue
		}
		if strings.HasPrefix(a.val, "--") {
			if a.is("--message", "--file") || strings.HasPrefix(a.val, "--message=") || strings.HasPrefix(a.val, "--file=") {
				return true
			}
		} else if strings.HasPrefix(a.val, "-m") || strings.HasPrefix(a.val, "-F") {
			return true
		}
	}
	return false
}

// ghPublishedFiles reports whether a gh invocation publishes text and the
// files it reads that text from, resolved against cwd.
func ghPublishedFiles(args []word, cwd string) (bool, []string) {
	i := 1
	for ; i < len(args); i++ {
		a := args[i]
		if !a.known || !strings.HasPrefix(a.val, "-") {
			break
		}
		if a.is("-R", "--repo") {
			i++
		}
	}
	if i >= len(args) {
		return false, nil
	}
	cmd := args[i]
	var sub word
	if i+1 < len(args) {
		sub = args[i+1]
	}
	var fileFlag string
	switch {
	case cmd.is("pr") && sub.is("create", "edit", "comment", "review"):
		fileFlag = "--body-file"
	case cmd.is("issue") && sub.is("create", "comment", "edit"):
		fileFlag = "--body-file"
	case cmd.is("release") && sub.is("create", "edit"):
		fileFlag = "--notes-file"
	case cmd.is("api"):
		return true, resolvePaths(ghAPIFieldFiles(args[i+1:]), cwd)
	default:
		return false, nil
	}
	rest := args[i+2:]
	var files []string
	for j := range rest {
		if p, ok := valueOf(rest, j, fileFlag); ok {
			files = append(files, p)
		} else if rest[j].is("-F") && j+1 < len(rest) && rest[j+1].known {
			files = append(files, rest[j+1].val)
		}
	}
	return true, resolvePaths(files, cwd)
}

// ghAPIFieldFiles picks the path out of each key=@path field; a plain
// key=value field reads no file.
func ghAPIFieldFiles(args []word) []string {
	var files []string
	for j, a := range args {
		var field string
		var ok bool
		if a.is("-f", "-F") && j+1 < len(args) && args[j+1].known {
			field, ok = args[j+1].val, true
		} else {
			for _, flag := range []string{"--field", "--raw-field"} {
				if field, ok = valueOf(args, j, flag); ok {
					break
				}
			}
		}
		if !ok {
			continue
		}
		if key, value, _ := strings.Cut(field, "="); key != "" && strings.HasPrefix(value, "@") {
			files = append(files, strings.TrimPrefix(value, "@"))
		}
	}
	return files
}

// valueOf reads the value of a long flag at args[j], given either as
// `--flag=value` or as `--flag value`.
func valueOf(args []word, j int, flag string) (string, bool) {
	if v, ok := args[j].hasPrefix(flag + "="); ok {
		return v, true
	}
	if args[j].is(flag) && j+1 < len(args) && args[j+1].known {
		return args[j+1].val, true
	}
	return "", false
}

func joinPath(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

func resolvePaths(paths []string, dir string) []string {
	var out []string
	for _, p := range paths {
		if p == "" || p == "-" {
			continue
		}
		out = append(out, joinPath(dir, p))
	}
	return out
}

// publishedFiles walks a parsed command and reports whether any git or gh
// call in it publishes text, along with every file those calls read text
// from. A call counts wherever it sits: in a list, pipeline, subshell or
// command substitution.
func publishedFiles(file *syntax.File, cwd string) (bool, []string) {
	publishes := false
	var files []string
	syntax.Walk(file, func(n syntax.Node) bool {
		call, ok := n.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		args := make([]word, len(call.Args))
		for i, w := range call.Args {
			args[i] = resolveWord(w)
		}
		if !args[0].known {
			return true
		}
		var p bool
		var f []string
		switch filepath.Base(args[0].val) {
		case "git":
			p, f = gitPublishedFiles(args, cwd)
		case "gh":
			p, f = ghPublishedFiles(args, cwd)
		}
		publishes = publishes || p
		files = append(files, f...)
		return true
	})
	return publishes, files
}

var gitOrGhWordRe = regexp.MustCompile(`\b(?:git|gh)\b`)

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
	// The whole command text is inspected, not only the publishing call's
	// arguments: `echo … | git commit -F -` carries its message outside
	// the call.
	texts := []string{command}
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
	if err != nil {
		// Writing the command in syntax the parser rejects, such as a
		// zsh-only construct, is not a way past the guard: any mention of
		// git or gh is judged on the raw text alone.
		if !gitOrGhWordRe.MatchString(command) {
			return Decision{}
		}
	} else {
		publishes, files := publishedFiles(file, hook.CWD)
		if !publishes {
			return Decision{}
		}
		for _, path := range files {
			content, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			texts = append(texts, string(content))
		}
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
