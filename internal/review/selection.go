package review

import (
	"errors"
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

		// Every condition is evaluated, including the ones after an unreadable
		// one. An `all:` rule that a readable condition already answered `no`
		// does not apply to this change, and whether some other clause could
		// have been read is then beside the point — a rule guarded to the
		// other context is the ordinary case.
		unreadable := ""
		anyHeld, anyRefused, contextRefused := false, false, false
		for _, cond := range conds {
			held, err := Evaluate(cond, p, ctx)
			if err != nil {
				if unreadable == "" {
					unreadable = err.Error()
				}
				continue
			}
			results = append(results, ConditionResult{Condition: cond, Held: held})
			if held {
				anyHeld = true
			} else {
				anyRefused = true
				if cond.Key == KeyContext {
					contextRefused = true
				}
			}
		}

		// An `all:` rule needs every condition, so one that could not be read
		// stops it. An `any:` rule fires on one, so an unreadable clause costs
		// it nothing as long as another holds.
		fired := anyHeld
		if all {
			fired = !anyRefused && unreadable == "" && len(results) == len(conds)
		}

		// Undecided means the unreadable clause is what stopped the rule.
		//
		// Only a context guard settles it, not any readable `no`. A rule this
		// context does not run in is not addressed to this review at all,
		// while a rule whose other clause merely did not match is still a
		// protection the repo asked for — and one that cannot be evaluated has
		// to say so on the first review rather than on whichever later one
		// happens to touch the right paths.
		undecided := unreadable != "" && !fired && !(all && contextRefused)
		if undecided && !m.Builtin {
			// The remedy belongs here and not in Evaluate: it is advice only a
			// repo that wrote the rule can take.
			return nil, fmt.Errorf("escalate[%d]: %s. Remove the rule, or narrow it with `touches`, rather than leaving an escalation that can never fire", i, unreadable)
		}
		if !fired {
			// A rule that could not be read is reported rather than dropped:
			// a rule that quietly never fires is what leaves a repo believing
			// it has a protection it does not.
			if undecided {
				sel.Skipped = append(sel.Skipped, SkippedRule{Index: i, To: rule.To, Reason: unreadable})
			}
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
func Evaluate(c Condition, p *Profile, ctx Context) (bool, error) {
	switch c.Key {
	case KeyContext:
		// Never unavailable: the context is what the caller asked for, not
		// something read off the change. It is the one condition that cannot
		// fail to be evaluated.
		return evaluateContext(c, ctx), nil

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
//
// A signal that could not be determined is only an obstacle when it could
// still change the answer. `in:` is satisfied by any one member, so a signal
// found present decides the condition however many of its siblings are
// unknown — and refusing there would turn a change that demonstrably carries
// `concurrency` into a shallower review because the blame budget ran out
// measuring something else.
func evaluateSignals(c Condition, p *Profile) (bool, error) {
	undetermined := ""

	for _, sig := range c.Signals {
		has, known := p.Signals.Has(sig)
		if !known {
			if undetermined == "" {
				undetermined = fmt.Sprintf("signal `%s` could not be determined for this change: %s (a signal that could not be read is not a signal the change does not carry)",
					sig, p.Signals.Undetermined(sig))
			}
			continue
		}
		switch c.Operator {
		case OpIn:
			// One member present settles it.
			if has {
				return true, nil
			}
		case OpNotIn:
			// One member present settles it.
			if has {
				return false, nil
			}
		case OpAllIn:
			// One member absent settles it.
			if !has {
				return false, nil
			}
		default:
			return false, fmt.Errorf("%q is not an operator for %s", c.Operator, c.Key)
		}
	}

	// Nothing settled the condition, so the unknown members are what stands
	// between here and an answer.
	if undetermined != "" {
		return false, errors.New(undetermined)
	}
	switch c.Operator {
	case OpIn:
		return false, nil
	case OpNotIn, OpAllIn:
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

// evaluateContext reports whether the review's context is one the condition
// names.
func evaluateContext(c Condition, ctx Context) bool {
	found := false
	for _, named := range c.Contexts {
		if named == ctx {
			found = true
			break
		}
	}
	if c.Operator == OpNotIn {
		return !found
	}
	return found
}
