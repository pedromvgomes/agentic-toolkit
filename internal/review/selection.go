package review

import (
	"fmt"
	"strings"
)

// Cost is how many model runs a panel spends: one per reviewer per quorum
// instance.
//
// It is also the panel ordering. "Rules only raise" needs a total order over
// panels, and panels carry only names — so the order is what a panel actually
// spends, which is the thing "deeper" already meant. A declared rank would be
// a second thing to keep true, and a manifest whose `deep` panel is cheaper
// than its `standard` one would then be ordered by an adjective rather than by
// what it does.
func (p Panel) Cost() int { return len(p.Reviewers) * p.EffectiveQuorum() }

// deeper reports whether panel a is a raise over panel b.
//
// Ties are not raises. Two panels that spend the same are the same depth, and
// a rule pointing at the second would otherwise swap one for the other for no
// reason a reader could see.
func deeper(a, b Panel) bool { return a.Cost() > b.Cost() }

// ConditionResult is one clause and whether it held.
type ConditionResult struct {
	Condition Condition
	Held      bool
}

// FiredRule is one escalation that fired, and what made it.
type FiredRule struct {
	// Index is the rule's position in the manifest, which is how --explain
	// names it. It is an address, not a precedence: every rule is evaluated
	// and the highest target wins.
	Index int
	To    string
	// All reports which combinator the rule carried.
	All        bool
	Conditions []ConditionResult
}

// String renders the rule the way --explain lists it.
func (f FiredRule) String() string {
	combinator := "any"
	if f.All {
		combinator = "all"
	}
	held := make([]string, 0, len(f.Conditions))
	for _, c := range f.Conditions {
		if c.Held {
			held = append(held, c.Condition.String())
		}
	}
	return fmt.Sprintf("escalate[%d] %s → %s: %s", f.Index, combinator, f.To, strings.Join(held, "; "))
}

// SkippedRule is an escalation that could not be evaluated against this
// change, and why.
//
// Only the built-in default produces these. A rule a repo wrote is refused
// instead, because a protection a repo asked for and cannot get is something
// it has to be told about rather than something to work around.
type SkippedRule struct {
	Index  int
	To     string
	Reason string
}

func (s SkippedRule) String() string {
	return fmt.Sprintf("escalate[%d] → %s: %s", s.Index, s.To, s.Reason)
}

// Selection is which panel runs, and every step of how that was decided.
type Selection struct {
	Context Context
	// Default is the panel the context starts from.
	Default string
	// Panel is what runs.
	Panel string
	// Fired lists every rule that held, whether or not it is the one that
	// decided the panel — a rule that fired and was already covered by a
	// deeper one is still something the author should be able to see.
	Fired []FiredRule
	// Overridden records that a caller named the panel outright, in which
	// case the default and the rules are reported but did not decide.
	Overridden bool
	// Skipped lists the rules that could not be evaluated against this change.
	// They are reported rather than silently dropped: a rule that never fires
	// is exactly what leaves a repo believing it has a protection it does not.
	Skipped []SkippedRule
	// Validates reports whether findings go to the validator.
	Validates bool
}

// Select decides which panel runs against this change.
//
// Every rule is evaluated. Order carries no meaning, and the highest target
// among those that fired wins — but never below the context's default, because
// escalations only raise. A mistaken rule can cost money and can never produce
// a shallower review than the repo asked for.
//
// An override names the panel outright and skips the arithmetic, but not the
// reporting: the default and the rules that fired are still recorded, so
// --explain can show what would have happened.
func Select(m *Manifest, ctx Context, p *Profile, override string) (*Selection, error) {
	sel := &Selection{Context: ctx, Default: m.Defaults.Default(ctx)}

	if override != "" {
		if _, ok := m.Panels[override]; !ok {
			return nil, fmt.Errorf("%q is not a panel this manifest declares; declared: %s",
				override, strings.Join(sortedMapKeys(m.Panels), ", "))
		}
		sel.Panel = override
		sel.Overridden = true
	} else {
		sel.Panel = sel.Default
	}

	for i, rule := range m.Escalate {
		conds, all := rule.Conditions()
		results := make([]ConditionResult, 0, len(conds))
		fired := all
		skipped := false
		for _, cond := range conds {
			held, err := Evaluate(cond, p)
			if err != nil {
				if !m.Builtin {
					// The remedy belongs here and not in Evaluate: it is
					// advice only a repo that wrote the rule can take.
					return nil, fmt.Errorf("escalate[%d]: %w. Remove the rule, or narrow it with `touches`, rather than leaving an escalation that can never fire", i, err)
				}
				sel.Skipped = append(sel.Skipped, SkippedRule{Index: i, To: rule.To, Reason: err.Error()})
				skipped = true
				break
			}
			results = append(results, ConditionResult{Condition: cond, Held: held})
			if all {
				fired = fired && held
			} else {
				fired = fired || held
			}
		}
		if skipped || !fired {
			continue
		}
		sel.Fired = append(sel.Fired, FiredRule{Index: i, To: rule.To, All: all, Conditions: results})
		if !sel.Overridden && deeper(m.Panels[rule.To], m.Panels[sel.Panel]) {
			sel.Panel = rule.To
		}
	}

	panel := m.Panels[sel.Panel]
	// A context that posts always validates, whatever the panel says. A false
	// finding there is published and blocks approval, rather than merely
	// cluttering a terminal.
	sel.Validates = ctx.Posts() || (panel.Validate != nil && *panel.Validate)
	return sel, nil
}

// Evaluate reports whether one condition holds for a change.
//
// A condition over a value the change could not produce is an error rather
// than a false: a rule that silently never fires leaves a repo believing it
// has a protection it does not have, which is the failure the whole
// unavailable-is-never-low rule exists for.
func Evaluate(c Condition, p *Profile) (bool, error) {
	switch c.Key {
	case KeyTouches:
		return evaluateTouches(c, p), nil

	case KeySignals:
		return evaluateSignals(c, p)

	case KeyChangedLines:
		return compare(c.Operator, p.ChangedLines, c.Number), nil

	case KeyChangedFiles:
		return compare(c.Operator, p.ChangedFiles, c.Number), nil

	case KeyReferencingFiles:
		n, ok := p.ReferencingFiles.Value()
		if !ok {
			return false, fmt.Errorf("`%s` cannot be read for this change: %s",
				KeyReferencingFiles, p.ReferencingFiles.Reason)
		}
		return compare(c.Operator, n, c.Number), nil
	}
	return false, fmt.Errorf("%q is not a condition key", c.Key)
}

// evaluateTouches matches globs against the reviewable paths.
//
// not_matches holds when NO reviewable path matches, which is the negation of
// the whole condition rather than of each path. A per-path negation would hold
// for any change big enough to include one unrelated file.
func evaluateTouches(c Condition, p *Profile) bool {
	matched := false
	for _, f := range p.ReviewableFiles() {
		if MatchAnyGlob(c.Globs, f.Path) {
			matched = true
			break
		}
	}
	if c.Operator == OpNotMatches {
		return !matched
	}
	return matched
}

// evaluateSignals tests the change's signals against the named ones.
func evaluateSignals(c Condition, p *Profile) (bool, error) {
	present := make([]bool, len(c.Signals))
	for i, sig := range c.Signals {
		has, known := p.Signals.Has(sig)
		if !known {
			return false, fmt.Errorf("signal `%s` could not be determined for this change: %s (a signal that could not be read is not a signal the change does not carry)",
				sig, p.Signals.Undetermined(sig))
		}
		present[i] = has
	}

	switch c.Operator {
	case OpIn:
		for _, has := range present {
			if has {
				return true, nil
			}
		}
		return false, nil
	case OpNotIn:
		for _, has := range present {
			if has {
				return false, nil
			}
		}
		return true, nil
	case OpAllIn:
		for _, has := range present {
			if !has {
				return false, nil
			}
		}
		return true, nil
	}
	return false, fmt.Errorf("%q is not an operator for %s", c.Operator, c.Key)
}

// compare applies a numeric operator.
func compare(op Operator, got, want int) bool {
	switch op {
	case OpGT:
		return got > want
	case OpGTE:
		return got >= want
	case OpLT:
		return got < want
	case OpLTE:
		return got <= want
	case OpEq:
		return got == want
	}
	return false
}
