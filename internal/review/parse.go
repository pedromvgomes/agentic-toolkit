package review

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

// ParseFile reads and decodes a review manifest from the local filesystem.
func ParseFile(filePath string) (*Manifest, error) {
	raw, err := os.ReadFile(filePath) // #nosec G304 -- parses the review manifest at the path the invoker named
	if err != nil {
		return nil, &ParseError{Path: filePath, Kind: ErrIO, Message: err.Error(), Wrapped: err}
	}
	return ParseBytes(filePath, raw)
}

// ParseBytes decodes raw YAML bytes into a Manifest and validates that it
// describes a review that could actually be staffed. filePath is used only in
// diagnostics.
//
// Decoding is strict: a key this build does not know is a hard error, not an
// ignored field. A manifest is read whole, and a typo silently dropped is a
// reviewer that never runs on a repo that believes it declared one.
func ParseBytes(filePath string, raw []byte) (*Manifest, error) {
	var m Manifest
	dec := yaml.NewDecoder(bytes.NewReader(raw), yaml.Strict())
	if err := dec.Decode(&m); err != nil {
		line, col := extractYAMLPos(err)
		return nil, &ParseError{
			Path:    filePath,
			Line:    line,
			Column:  col,
			Kind:    classifyYAMLError(err),
			Message: cleanYAMLMessage(err),
			Wrapped: err,
		}
	}
	if err := m.validate(filePath); err != nil {
		return nil, err
	}
	return &m, nil
}

// validate checks everything the type system and the strict decode cannot:
// that the names a manifest uses are names it declares, and that every rule
// could fire.
//
// Each check is a refusal rather than a warning. A panel naming a reviewer
// that does not exist cannot staff itself, and a rule pointing at a panel that
// does not exist can never raise anything — both leave a repo believing it has
// a review it does not have.
func (m *Manifest) validate(filePath string) error {
	if m.Version != SchemaVersion {
		return fieldErr(filePath, "version", ErrUnknownVersion,
			"manifest version %d is not %d, the only version this build reads", m.Version, SchemaVersion)
	}

	if len(m.Reviewers) == 0 {
		return fieldErr(filePath, "reviewers", ErrMissingRequired,
			"a manifest declares at least one reviewer")
	}
	for _, name := range sortedMapKeys(m.Reviewers) {
		runner := m.Reviewers[name]
		if err := runner.validate(filePath, "reviewers."+name); err != nil {
			return err
		}
		m.Reviewers[name] = runner
	}
	// The judge and the validator are declared absent-able only so the YAML
	// can leave them out; a manifest missing either describes a review that
	// cannot complete, and saying so here beats discovering it after a panel
	// has been spent.
	if m.Judge == nil {
		return fieldErr(filePath, "judge", ErrMissingRequired,
			"a manifest declares a judge; without one nothing decides which findings survive")
	}
	if err := m.Judge.validate(filePath, "judge"); err != nil {
		return err
	}
	if m.Validator != nil {
		if err := m.Validator.validate(filePath, "validator"); err != nil {
			return err
		}
	}

	if len(m.Panels) == 0 {
		return fieldErr(filePath, "panels", ErrMissingRequired,
			"a manifest declares at least one panel")
	}
	for _, name := range sortedMapKeys(m.Panels) {
		panel := m.Panels[name]
		field := "panels." + name
		if len(panel.Reviewers) == 0 {
			return fieldErr(filePath, field+".reviewers", ErrMissingRequired,
				"a panel names at least one reviewer, or it cannot staff itself")
		}
		for i, reviewer := range panel.Reviewers {
			if _, ok := m.Reviewers[reviewer]; !ok {
				return fieldErr(filePath, fmt.Sprintf("%s.reviewers[%d]", field, i), ErrUnknownName,
					"%q is not a reviewer this manifest declares; declared: %s",
					reviewer, strings.Join(sortedMapKeys(m.Reviewers), ", "))
			}
		}
		if panel.Quorum < 0 {
			return fieldErr(filePath, field+".quorum", ErrInvalidPanel,
				"a quorum of %d runs nothing; omit it for one instance of each reviewer", panel.Quorum)
		}
		if panel.Judge != nil {
			if err := panel.Judge.validate(filePath, field+".judge"); err != nil {
				return err
			}
		}
		if panel.Validator != nil {
			if err := panel.Validator.validate(filePath, field+".validator"); err != nil {
				return err
			}
		}
		if panel.Validate != nil && *panel.Validate && m.EffectiveValidator(name) == nil {
			return fieldErr(filePath, field+".validate", ErrMissingRequired,
				"this panel validates, but neither it nor the manifest declares a validator")
		}
	}

	for _, ctx := range Contexts {
		name := m.Defaults.Default(ctx)
		field := "defaults." + string(ctx)
		if name == "" {
			return fieldErr(filePath, field, ErrMissingRequired,
				"every context names the panel it starts from")
		}
		if _, ok := m.Panels[name]; !ok {
			return fieldErr(filePath, field, ErrUnknownName,
				"%q is not a panel this manifest declares; declared: %s",
				name, strings.Join(sortedMapKeys(m.Panels), ", "))
		}
		// A context that posts always validates, so it needs a validator
		// whatever its panels say. A false finding on a PR is published and
		// blocks approval, rather than merely cluttering a terminal.
		//
		// Every panel is checked, not the context's default alone: an
		// escalation raises to another panel and --panel names any of them, so
		// a panel that resolves no validator is a review that cannot post,
		// discovered when the rule that raised to it fires rather than now.
		if ctx.Posts() {
			for _, panelName := range sortedMapKeys(m.Panels) {
				if m.EffectiveValidator(panelName) == nil {
					return fieldErr(filePath, "panels."+panelName+".validator", ErrMissingRequired,
						"the %s context posts, and a context that posts always validates, so panel %q needs a validator: declare one on the panel or on the manifest",
						ctx, panelName)
				}
			}
		}
	}

	for i, pattern := range m.Exclude {
		field := fmt.Sprintf("exclude[%d]", i)
		switch {
		case strings.TrimSpace(pattern) == "":
			return fieldErr(filePath, field, ErrMissingRequired,
				"an exclusion names a path glob; an empty one matches nothing and reads as a rule")
		case strings.HasPrefix(pattern, "/"):
			// git names paths from the repository root with no leading
			// separator, so this pattern can never match. A rule that silently
			// matches nothing is worse than no rule: the repo believes a path
			// is excluded and every review reads it.
			return fieldErr(filePath, field, ErrUnknownName,
				"%q starts with %q and paths are named from the repository root without one, so it would match nothing; write %q",
				pattern, "/", strings.TrimPrefix(pattern, "/"))
		case pattern == "**" || pattern == "*":
			// Excluding everything empties the review, and a review that found
			// nothing reads exactly like a review of nothing — which is what
			// unblocks approval.
			return fieldErr(filePath, field, ErrUnknownName,
				"%q excludes every file, which produces an empty review rather than a clean one; name the paths to skip", pattern)
		}
	}

	// A floor is refused rather than defaulted when it is not a rung. A word
	// off the ladder ranks below every severity, so an unchecked one would
	// oblige nothing and grant approval over every finding on the pull
	// request — silently, in the repo that took the trouble to set it.
	if floor := m.Approval.Floor; floor != "" && !floor.Valid() {
		return fieldErr(filePath, "approval.floor", ErrUnknownName,
			"%q is not a severity; use one of %s", floor, strings.Join(SeverityNames(), ", "))
	}

	for i, rule := range m.Escalate {
		field := fmt.Sprintf("escalate[%d]", i)
		if rule.To == "" {
			return fieldErr(filePath, field+".to", ErrMissingRequired,
				"an escalation names the panel it raises to")
		}
		if _, ok := m.Panels[rule.To]; !ok {
			return fieldErr(filePath, field+".to", ErrUnknownName,
				"%q is not a panel this manifest declares; declared: %s",
				rule.To, strings.Join(sortedMapKeys(m.Panels), ", "))
		}
		switch {
		case len(rule.All) > 0 && len(rule.Any) > 0:
			return fieldErr(filePath, field, ErrInvalidCondition,
				"a rule carries exactly one of `all:` or `any:`; this one carries both. Disjunction between two conjunctions is two rules")
		case len(rule.All) == 0 && len(rule.Any) == 0:
			return fieldErr(filePath, field, ErrMissingRequired,
				"a rule carries exactly one of `all:` or `any:`, with at least one condition under it")
		}
	}

	return nil
}

// validate checks one runner and parses its prompt reference.
func (r *Runner) validate(filePath, field string) error {
	if r.Provider == "" {
		return fieldErr(filePath, field+".provider", ErrMissingRequired,
			"a run names the coding-agent CLI it is driven through")
	}
	if r.Prompt.Raw == "" {
		return fieldErr(filePath, field+".prompt", ErrMissingRequired,
			"a run names the prompt body it is given")
	}
	parsed, err := ParsePromptRef(r.Prompt.Raw)
	if err != nil {
		return wrapFieldErr(filePath, field+".prompt", ErrInvalidPrompt, err)
	}
	r.Prompt = parsed
	return nil
}

// UnmarshalYAML stashes the verbatim string in Raw. The real parse runs during
// validation, so a diagnostic can name the field the reference came from
// rather than a line the strict decoder happened to be on.
func (p *PromptRef) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return fmt.Errorf("a prompt is a string, either `builtin:<name>` or `./<path>`: %w", err)
	}
	p.Raw = s
	return nil
}

// ParsePromptRef parses one `prompt:` value.
//
// Two forms and no third. A bare name would have to be resolved against
// something, and the only candidates are the builtin set and the repo — which
// is a lookup order that decides silently which body a reviewer got.
func ParsePromptRef(raw string) (PromptRef, error) {
	out := PromptRef{Raw: raw}
	switch {
	case raw == "":
		return PromptRef{}, fmt.Errorf("prompt is empty")

	case strings.HasPrefix(raw, builtinPrefix):
		name := strings.TrimPrefix(raw, builtinPrefix)
		if name == "" {
			return PromptRef{}, fmt.Errorf("%q names no builtin prompt", raw)
		}
		if !IsBuiltinPrompt(name) {
			return PromptRef{}, fmt.Errorf("%q is not a prompt that ships with agtk; the built-in ones are %s",
				name, strings.Join(BuiltinPrompts, ", "))
		}
		out.Kind = PromptBuiltin
		out.Name = name
		return out, nil

	case strings.HasPrefix(raw, "./"):
		// Cleaned and checked here rather than at read time: a path that
		// climbs out of the manifest's directory reaches code the branch
		// under review can write, which is the one place a reviewer's
		// instructions must never come from.
		cleaned := path.Clean(filepath.ToSlash(raw))
		if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) {
			return PromptRef{}, fmt.Errorf("prompt path %q must stay inside %s", raw, ManifestDir)
		}
		out.Kind = PromptPath
		out.Path = cleaned
		return out, nil
	}
	return PromptRef{}, fmt.Errorf("prompt %q is neither `builtin:<name>` nor a path starting with `./`", raw)
}

// UnmarshalYAML decodes one `key: {operator: value}` clause.
//
// The one-key-per-level rule is enforced by counting rather than by the struct
// tags, because yaml.Strict() does not reach inside a mapping decoded
// dynamically — and counting is the stronger check anyway: it is what makes a
// two-key mapping an error instead of a silently inferred AND.
func (c *Condition) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var outer yaml.MapSlice
	if err := unmarshal(&outer); err != nil {
		return fmt.Errorf("a condition is one `key: {operator: value}` mapping: %w", err)
	}
	if len(outer) != 1 {
		if len(outer) == 0 {
			return fmt.Errorf("a condition is one `key: {operator: value}` mapping; this one is empty")
		}
		return fmt.Errorf("a condition is one `key: {operator: value}` mapping; this one has %d keys (%s). Two conditions in the rule's `all:` is the conjunction",
			len(outer), sortedKeys(mapSliceKeys(outer)))
	}

	name := fmt.Sprint(outer[0].Key)
	key := ConditionKey(name)
	if !key.Known() {
		return errUnknownKey(name)
	}

	inner, ok := outer[0].Value.(map[string]interface{})
	if !ok {
		return fmt.Errorf("%s takes `{operator: value}`, not a bare value; write one of %s — for example `%s: {%s: …}`",
			key, listOperators(key), key, key.Operators()[0])
	}
	if len(inner) != 1 {
		if len(inner) == 0 {
			return fmt.Errorf("%s takes exactly one operator; this one has none. Valid: %s", key, listOperators(key))
		}
		return fmt.Errorf("%s takes exactly one operator; this one has %d (%s). Two conditions in the rule's `all:` is the conjunction",
			key, len(inner), sortedKeys(mapKeys(inner)))
	}

	var opName string
	var value interface{}
	for k, v := range inner {
		opName, value = k, v
	}
	op := Operator(opName)
	if !key.Accepts(op) {
		return errWrongOperator(key, opName)
	}

	c.Raw = fmt.Sprintf("%s: {%s: …}", key, op)
	c.Key = key
	c.Operator = op

	switch conditionKeys[key].Operand {
	case operandGlobs:
		globs, err := decodeStrings(value)
		if err != nil {
			return fmt.Errorf("%s: %s takes globs: %w", key, op, err)
		}
		c.Globs = globs
		c.Raw = fmt.Sprintf("%s: {%s: [%s]}", key, op, strings.Join(globs, ", "))

	case operandSignals:
		names, err := decodeStrings(value)
		if err != nil {
			return fmt.Errorf("%s: %s takes signal names: %w", key, op, err)
		}
		for _, n := range names {
			sig := Signal(n)
			if !sig.Known() {
				return errUnknownSignal(n)
			}
			c.Signals = append(c.Signals, sig)
		}
		c.Raw = fmt.Sprintf("%s: {%s: [%s]}", key, op, strings.Join(names, ", "))

	case operandInt:
		n, err := decodeInt(value)
		if err != nil {
			return fmt.Errorf("%s: %s takes a whole number: %w", key, op, err)
		}
		c.Number = n
		c.Raw = fmt.Sprintf("%s: {%s: %d}", key, op, n)
	}
	return nil
}

// decodeStrings reads an operator's value as a list of strings, accepting a
// single string as a one-member list.
//
// A list means "any of", which the operator's own name is what forces:
// `matches` over three globs holds when one matches, and conjunction over
// globs is two members of the rule's `all:`. Conjunction over a set has its
// own operator, `all_in`.
func decodeStrings(v interface{}) ([]string, error) {
	switch val := v.(type) {
	case string:
		if val == "" {
			return nil, fmt.Errorf("the value is empty")
		}
		return []string{val}, nil
	case []interface{}:
		if len(val) == 0 {
			return nil, fmt.Errorf("the list is empty, so the condition can never hold")
		}
		out := make([]string, 0, len(val))
		for _, item := range val {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%v is not a string", item)
			}
			if s == "" {
				return nil, fmt.Errorf("the list has an empty member")
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%v is neither a string nor a list of strings", v)
}

// decodeInt reads an operator's value as a count.
//
// Counts are never negative, and a rule comparing one against a negative
// number either always holds or never does — which is a rule that reads as a
// protection and is not one.
//
// The same is true of a number too large to hold. A value past the platform's
// int silently becomes a different number, and on a 32-bit build
// `changed_files: {gte: 4294967296}` would land on zero and make the rule fire
// on every change — so the range is checked before the conversion rather than
// the result inspected afterwards.
func decodeInt(v interface{}) (int, error) {
	var n int
	switch val := v.(type) {
	case int:
		n = val
	case int64:
		if val > math.MaxInt || val < math.MinInt {
			return 0, fmt.Errorf("%d is outside the range a count can take", val)
		}
		n = int(val)
	case uint64:
		if val > math.MaxInt {
			return 0, fmt.Errorf("%d is larger than any count a change can produce", val)
		}
		n = int(val)
	case uint:
		if val > math.MaxInt {
			return 0, fmt.Errorf("%d is larger than any count a change can produce", val)
		}
		n = int(val)
	default:
		return 0, fmt.Errorf("%v is not a whole number", v)
	}
	if n < 0 {
		return 0, fmt.Errorf("%d is negative, and a count never is", n)
	}
	return n, nil
}

// mapSliceKeys renders an ordered mapping's keys as strings.
func mapSliceKeys(m yaml.MapSlice) []string {
	out := make([]string, 0, len(m))
	for _, item := range m {
		out = append(out, fmt.Sprint(item.Key))
	}
	return out
}

// mapKeys renders a mapping's keys as strings.
func mapKeys(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// sortedMapKeys returns a map's keys in a stable order. Reviewer and panel
// names are identities and their declaration order carries no meaning, so
// every place that reads them sorts, and no diagnostic depends on which way
// the runtime happened to walk a map.
func sortedMapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ===== YAML error helpers =====
//
// ErrUnknownField and the position both come from matching goccy's own error
// text, because the library reports neither through a typed error. A version
// bump that rewords the message degrades the kind to ErrYAMLSyntax and
// collapses the position to 0:0 without failing to compile, so both are worth
// re-checking deliberately after one.

func classifyYAMLError(err error) ErrorKind {
	if strings.Contains(err.Error(), "unknown field") {
		return ErrUnknownField
	}
	return ErrYAMLSyntax
}

var yamlPosRE = regexp.MustCompile(`\[(\d+):(\d+)\]`)

func extractYAMLPos(err error) (int, int) {
	if se, ok := err.(*yaml.SyntaxError); ok {
		if t := se.Token; t != nil && t.Position != nil {
			return t.Position.Line, t.Position.Column
		}
	}
	if m := yamlPosRE.FindStringSubmatch(err.Error()); len(m) == 3 {
		var l, c int
		fmt.Sscanf(m[1], "%d", &l) // #nosec G104 -- best-effort position; the parse error already returned is what matters
		fmt.Sscanf(m[2], "%d", &c) // #nosec G104 -- best-effort position; the parse error already returned is what matters
		return l, c
	}
	return 0, 0
}

func cleanYAMLMessage(err error) string {
	s := err.Error()
	if i := strings.Index(s, "\n"); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
