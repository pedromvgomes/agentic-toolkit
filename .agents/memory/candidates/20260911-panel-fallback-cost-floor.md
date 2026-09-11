---
about: a Panel's fallback: must cost at least as much as the panel declaring it, or manifest parsing refuses it
saw:
  - source/toolkit/internal/review/parse.go
  - source/toolkit/internal/review/selection.go
---

`source/toolkit/internal/review/parse.go`'s `validate()` compares `Panel.Cost()` (`selection.go`:
`len(p.Reviewers) * p.EffectiveQuorum()`, the same measure `deeper()` uses for escalation)
between a panel and the panel its `fallback:` names. A fallback whose cost is lower than the
panel declaring it is refused with `ErrInvalidPanel`; equal cost is accepted (the built-in
`default.yaml`'s provider twins are all equal cost), and a deeper fallback is accepted too.

This is deliberate, not an oversight to relax: an `Escalation` rule can raise a change to
`deep` because of a `signals` or `referencing_files` condition, and a `deep.fallback: quick`
would let a provider block silently downgrade that decision — the retry runs, answers, and
looks like a normal review while spending a fraction of what the rule asked for, with nothing
in the output saying so. The check exists at parse time specifically so this can never be
discovered only after a block actually happens.
