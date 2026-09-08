package review

import (
	"fmt"
	"sort"
	"strings"
)

// Explain renders how the panel was chosen: the default, every rule that
// fired, and what resulted.
//
// It reports rules that fired without deciding as well as the one that did. A
// rule already covered by a deeper one is still something its author should be
// able to see firing, and a repo debugging why a panel is deeper than expected
// needs the whole list rather than the winner.
func (s *Selection) Explain(m *Manifest, p *Profile) string {
	var b strings.Builder

	fmt.Fprintf(&b, "change:  %s\n", p.Summary())
	if langs := p.Languages(); len(langs) > 0 {
		names := make([]string, 0, len(langs))
		for _, lang := range langs {
			names = append(names, languageLabel(lang))
		}
		fmt.Fprintf(&b, "written: %s\n", strings.Join(names, ", "))
	}
	if len(p.Symbols) > 0 {
		fmt.Fprintf(&b, "symbols: %s\n", strings.Join(p.Symbols, ", "))
	}

	fmt.Fprintf(&b, "context: %s\n", s.Context)
	fmt.Fprintf(&b, "default: %s%s\n", s.Default, PanelShape(m, s.Default))

	if len(s.Fired) == 0 {
		fmt.Fprintf(&b, "fired:   nothing\n")
	}
	for i, rule := range s.Fired {
		label := "fired:  "
		if i > 0 {
			label = "        "
		}
		fmt.Fprintf(&b, "%s %s\n", label, rule)
	}

	for i, rule := range s.Skipped {
		label := "skipped:"
		if i > 0 {
			label = "        "
		}
		fmt.Fprintf(&b, "%s %s\n", label, rule)
	}

	if s.Overridden {
		fmt.Fprintf(&b, "panel:   %s%s (named on the command line; the rules above did not decide)\n",
			s.Panel, PanelShape(m, s.Panel))
	} else {
		fmt.Fprintf(&b, "panel:   %s%s\n", s.Panel, PanelShape(m, s.Panel))
	}
	// The panel's own description, where its author wrote one. The shape above
	// says what the panel spends; this is the only line that says what it is
	// for, which is what a reader deciding whether to override it needs.
	if desc := m.Panels[s.Panel].Description; desc != "" {
		fmt.Fprintf(&b, "         %s\n", desc)
	}

	if s.Validates {
		fmt.Fprintf(&b, "validate: yes — %s\n", s.ValidationReason())
	} else {
		fmt.Fprintf(&b, "validate: no\n")
	}

	if excluded := p.ExcludedFiles(); len(excluded) > 0 {
		fmt.Fprintf(&b, "\nexcluded (%d):\n", len(excluded))
		for _, line := range summariseExclusions(excluded) {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	return b.String()
}

// ValidationReason says why findings go to the validator, or "" when they do
// not. A context that posts forces validation whatever the panel says, and
// the reason names which of the two asked.
func (s *Selection) ValidationReason() string {
	if !s.Validates {
		return ""
	}
	if s.Context.Posts() {
		return "the " + string(s.Context) + " context posts, and a context that posts always validates"
	}
	return "the panel asks for it"
}

// PanelShape renders what a panel costs, so the ordering escalations use is
// visible rather than something a reader has to infer from the names. One
// rendering serves every place a panel is listed, so a reader meets one shape
// for one panel wherever they see it.
func PanelShape(m *Manifest, name string) string {
	panel, ok := m.Panels[name]
	if !ok {
		return ""
	}
	runs := "run"
	if panel.Cost() != 1 {
		runs = "runs"
	}
	if panel.EffectiveQuorum() > 1 {
		return fmt.Sprintf(" (%d reviewers × quorum %d = %d %s)",
			len(panel.Reviewers), panel.EffectiveQuorum(), panel.Cost(), runs)
	}
	return fmt.Sprintf(" (%d %s)", panel.Cost(), runs)
}

// summariseExclusions groups excluded files by reason, so a dependency bump
// reports one line rather than four hundred.
func summariseExclusions(files []ChangedFile) []string {
	byReason := map[Exclusion][]string{}
	for _, f := range files {
		byReason[f.Excluded] = append(byReason[f.Excluded], f.Path)
	}
	reasons := make([]string, 0, len(byReason))
	for reason := range byReason {
		reasons = append(reasons, string(reason))
	}
	sort.Strings(reasons)

	out := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		paths := byReason[Exclusion(reason)]
		sort.Strings(paths)
		if len(paths) <= exclusionsListedPerReason {
			out = append(out, fmt.Sprintf("%s: %s", reason, strings.Join(paths, ", ")))
			continue
		}
		out = append(out, fmt.Sprintf("%s: %s, and %d more",
			reason, strings.Join(paths[:exclusionsListedPerReason], ", "), len(paths)-exclusionsListedPerReason))
	}
	return out
}

// exclusionsListedPerReason is how many paths are named before the rest are
// counted. Enough to recognise what was dropped, few enough that a vendored
// tree does not fill the terminal.
const exclusionsListedPerReason = 3
