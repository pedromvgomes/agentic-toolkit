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
	"slices"
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
	// A flag may sit between the command and its action word:
	// `gh pr -R owner/repo create`.
	s := i + 1
	for ; s < len(args); s++ {
		a := args[s]
		if !a.known || !strings.HasPrefix(a.val, "-") {
			break
		}
		if a.is("-R", "--repo") {
			s++
		}
	}
	var sub word
	if s < len(args) {
		sub = args[s]
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
	rest := args[s+1:]
	var files []string
	for j := range rest {
		if p, ok := valueOf(rest, j, fileFlag); ok {
			files = append(files, p)
		} else if rest[j].is("-F") && j+1 < len(rest) && rest[j+1].known {
			files = append(files, rest[j+1].val)
		} else if p, ok := rest[j].hasPrefix("-F"); ok && p != "" {
			files = append(files, p)
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
		} else if v, attached := a.hasPrefix("-F"); attached && v != "" {
			field, ok = v, true
		} else if v, attached := a.hasPrefix("-f"); attached && v != "" {
			field, ok = v, true
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

// optionSpec describes how a wrapper command reads its own options.
type optionSpec struct {
	// valued are the short options that take a value, attached (`-uroot`)
	// or as the next word (`-u root`).
	valued string
	// optional are the short options that take a value only when attached.
	optional string
	// long are the long options that take the next word as their value
	// when not given as `--name=value`.
	long []string
	// stop names the options after which reading ends, because they
	// rewrite the words that follow.
	stop []string
}

type option struct {
	name  string
	value word
}

// parseOptions reads the options that open args[i:] and returns them with
// the index of the first word that is not one. ok is false when an option
// position holds an unknown word: its expansion could be any number of
// options or the command itself, so the rest of the call is undetermined.
func parseOptions(args []word, i int, spec optionSpec) ([]option, int, bool) {
	var opts []option
	for ; i < len(args); i++ {
		a := args[i]
		if !a.known {
			return opts, i, false
		}
		if a.val == "--" {
			return opts, i + 1, true
		}
		if !strings.HasPrefix(a.val, "-") || a.val == "-" {
			return opts, i, true
		}
		if strings.HasPrefix(a.val, "--") {
			name, val, attached := strings.Cut(a.val, "=")
			o := option{name: name}
			if attached {
				o.value = word{val: val, known: true}
			} else if slices.Contains(spec.long, name) && i+1 < len(args) {
				i++
				o.value = args[i]
			}
			opts = append(opts, o)
			if slices.Contains(spec.stop, name) {
				return opts, i + 1, true
			}
			continue
		}
		cluster := a.val[1:]
		for j, r := range cluster {
			o := option{name: "-" + string(r)}
			rest := cluster[j+len(string(r)):]
			if strings.ContainsRune(spec.valued, r) {
				if rest != "" {
					o.value = word{val: rest, known: true}
				} else if i+1 < len(args) {
					i++
					o.value = args[i]
				}
				opts = append(opts, o)
				break
			}
			if strings.ContainsRune(spec.optional, r) {
				o.value = word{val: rest, known: rest != ""}
				opts = append(opts, o)
				break
			}
			opts = append(opts, o)
		}
		if last := opts[len(opts)-1]; slices.Contains(spec.stop, last.name) {
			return opts, i + 1, true
		}
	}
	return opts, i, true
}

// wrapper describes a command that runs the command named after its own
// options, such as `sudo` or `env`.
type wrapper struct {
	spec optionSpec
	// operands counts the words the wrapper reads between its options and
	// the command, such as timeout's duration.
	operands int
	// assignments is true when NAME=VALUE words may precede the command.
	assignments bool
	// chdir names the options whose value is the wrapped command's
	// working directory.
	chdir []string
	// query names the options under which the wrapper reports on the
	// command instead of running it, as `command -v` does.
	query []string
}

var wrappers = map[string]wrapper{
	"command": {query: []string{"-v", "-V"}},
	"builtin": {},
	"exec":    {spec: optionSpec{valued: "a"}},
	"nohup":   {},
	"time":    {spec: optionSpec{valued: "fo", long: []string{"--format", "--output"}}},
	"nice":    {spec: optionSpec{valued: "n", long: []string{"--adjustment"}}},
	"timeout": {
		spec:     optionSpec{valued: "ks", long: []string{"--kill-after", "--signal"}},
		operands: 1,
	},
	"env": {
		spec: optionSpec{
			valued: "uCSP",
			long:   []string{"--unset", "--chdir", "--split-string"},
			stop:   []string{"-S", "--split-string"},
		},
		assignments: true,
		chdir:       []string{"-C", "--chdir"},
	},
	"sudo": {
		spec: optionSpec{
			valued: "aCcDghprRtTUu",
			long: []string{
				"--auth-type", "--close-from", "--chdir", "--group", "--host", "--login-class",
				"--prompt", "--chroot", "--role", "--type", "--command-timeout", "--other-user", "--user",
			},
		},
		assignments: true,
		chdir:       []string{"-D", "--chdir"},
	},
	"doas": {spec: optionSpec{valued: "uC"}},
	"xargs": {spec: optionSpec{
		valued:   "adEILnPsJRS",
		optional: "eil",
		long:     []string{"--arg-file", "--delimiter", "--max-args", "--max-procs", "--max-chars", "--process-slot-var"},
	}},
}

// unwrap returns the command a wrapper call runs, with the directory that
// command runs in. ok is false when the wrapped command cannot be
// determined or the wrapper does not run it. When env -S's split string is
// known but cannot be read as a single command, text holds the split
// string and any arguments that would have followed it, for the caller to
// judge on their text instead.
func unwrap(w wrapper, args []word, cwd string) ([]word, string, string, bool) {
	i := 1
	for {
		opts, next, ok := parseOptions(args, i, w.spec)
		if !ok {
			return nil, "", "", false
		}
		i = next
		var split *word
		for _, o := range opts {
			if slices.Contains(w.query, o.name) {
				return nil, "", "", false
			}
			if slices.Contains(w.chdir, o.name) && o.value.known {
				cwd = joinPath(cwd, o.value.val)
			}
			if slices.Contains(w.spec.stop, o.name) {
				split = &o.value
			}
		}
		if split == nil {
			break
		}
		// env -S splits its value into words that take the option's
		// place, options and command included.
		if !split.known {
			return nil, "", "", false
		}
		words, ok := splitWords(split.val)
		if !ok {
			return nil, "", joinKnownWords(split.val, args[i:]), false
		}
		args = append(append([]word{args[0]}, words...), args[i:]...)
		i = 1
	}
	if filepath.Base(args[0].val) == "env" && i < len(args) && args[i].is("-") {
		i++
	}
	i += w.operands
	for w.assignments && i < len(args) {
		if !args[i].known {
			return nil, "", "", false
		}
		if !strings.Contains(args[i].val, "=") {
			break
		}
		i++
	}
	if i >= len(args) {
		return nil, "", "", false
	}
	return args[i:], cwd, "", true
}

// joinKnownWords joins text with the known words of args, for judging on
// text when the words cannot be read as a command.
func joinKnownWords(text string, args []word) string {
	parts := []string{text}
	for _, a := range args {
		if a.known {
			parts = append(parts, a.val)
		}
	}
	return strings.Join(parts, " ")
}

// splitWords reads text as the words of a single simple command.
func splitWords(text string) ([]word, bool) {
	file, err := parseCommand(text)
	if err != nil || len(file.Stmts) != 1 {
		return nil, false
	}
	call, ok := file.Stmts[0].Cmd.(*syntax.CallExpr)
	if !ok {
		return nil, false
	}
	words := make([]word, len(call.Args))
	for i, w := range call.Args {
		words[i] = resolveWord(w)
	}
	return words, true
}

var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true}

// shellPublishedFiles judges a shell started with `-c STRING` on the
// script STRING holds. A shell running a script file is not followed.
func shellPublishedFiles(args []word, cwd string) (bool, []string) {
	command := false
	i := 1
	for ; i < len(args); i++ {
		a := args[i]
		if !a.known {
			return false, nil
		}
		if a.is("--", "-") {
			i++
			break
		}
		if strings.HasPrefix(a.val, "--") {
			if a.is("--rcfile", "--init-file") {
				i++
			}
			continue
		}
		if len(a.val) < 2 || (a.val[0] != '-' && a.val[0] != '+') {
			break
		}
		cluster := a.val[1:]
		if a.val[0] == '-' && strings.ContainsRune(cluster, 'c') {
			command = true
		}
		if strings.ContainsAny(cluster, "oO") {
			i++
		}
	}
	if !command || i >= len(args) || !args[i].known {
		return false, nil
	}
	return scriptPublishedFiles(args[i].val, cwd)
}

// evalPublishedFiles judges an eval call on the script its arguments
// join into.
func evalPublishedFiles(args []word, cwd string) (bool, []string) {
	rest := args[1:]
	if len(rest) > 0 && rest[0].is("--") {
		rest = rest[1:]
	}
	parts := make([]string, len(rest))
	for i, a := range rest {
		if !a.known {
			return false, nil
		}
		parts[i] = a.val
	}
	return scriptPublishedFiles(strings.Join(parts, " "), cwd)
}

// scriptPublishedFiles judges a script handed to a shell as a string. A
// script the parser rejects is judged the way an unparseable command is:
// naming git or gh counts as publishing.
func scriptPublishedFiles(script, cwd string) (bool, []string) {
	file, err := parseCommand(script)
	if err != nil {
		return gitOrGhWordRe.MatchString(script), nil
	}
	return publishedFiles(file, cwd)
}

// callPublishedFiles reports whether one call publishes text and the files
// it reads that text from. Wrappers are looked through, nested to any
// depth, to the command they run.
func callPublishedFiles(args []word, cwd string) (bool, []string) {
	for len(args) > 0 && args[0].known {
		name := filepath.Base(args[0].val)
		switch {
		case name == "git":
			return gitPublishedFiles(args, cwd)
		case name == "gh":
			return ghPublishedFiles(args, cwd)
		case shells[name]:
			return shellPublishedFiles(args, cwd)
		case name == "eval":
			return evalPublishedFiles(args, cwd)
		}
		w, ok := wrappers[name]
		if !ok {
			return false, nil
		}
		var text string
		if args, cwd, text, ok = unwrap(w, args, cwd); !ok {
			return gitOrGhWordRe.MatchString(text), nil
		}
	}
	return false, nil
}

// publishedFiles walks a parsed command and reports whether any git or gh
// call in it publishes text, along with every file those calls read text
// from. A call counts wherever it sits: in a list, pipeline, subshell or
// command substitution, behind a wrapper such as `sudo` or `env`, or in a
// script handed to `bash -c` or `eval`.
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
		p, f := callPublishedFiles(args, cwd)
		publishes = publishes || p
		files = append(files, f...)
		return true
	})
	return publishes, files
}

func parseCommand(command string) (*syntax.File, error) {
	return syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
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
	file, err := parseCommand(command)
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
			content, err := os.ReadFile(path) // #nosec G304 -- reads the file the command publishes, wherever it names it
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
