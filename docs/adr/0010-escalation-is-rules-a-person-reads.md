# Escalation is rules a person reads, not a ladder a model climbs

A review manifest names the panel each context starts from, and a list of rules that raise it.
Every rule is evaluated, the highest target wins, and each rule says in one line what it fires
on. Nothing infers a depth from arithmetic over the change, and nothing about a rule's meaning
depends on where it sits in the document.

The caller has already decided. A person types `agtk code-review run` having just judged the
change ready, so a depth derived from size classes and signal counts is a depth the caller
already knew — computed less accurately, and unexplainable when it comes out wrong. What that
person needs is the opposite: `agtk code-review explain` naming the panel, the rules that
fired, and the profile they matched, for free and before any model runs.

The vocabulary a rule matches on ships with the binary. Detecting that a change touches
concurrency or crypto is language knowledge, and language knowledge has to be tested somewhere
other than a consumer's YAML. A repo's escape hatch is `touches`, which is honest about being
path-only.

## Considered options

**A rung ladder: size class, criticality signals and reference fan-in, combined into a depth.**
This is what the `deep-code-review` skill does, and it is right there — the skill exists to be
invoked by a *model*, from a session where nobody chose a depth, so something has to choose one.
Rejected here because the premise does not hold: a person typed the command. The ladder's
inputs survive as escalation conditions, where each stands alone and says what it does; the
size taxonomy and the arithmetic on top of them do not.

**Repo-declared content patterns.** Zones matching regexes against the diff, so a repo could
define `concurrency` for itself. Rejected: a repo that writes `sync\.Mutex` gets nothing the
day it adds another language, and nobody goes back to update it. A vocabulary that is wrong in
a way the repo cannot see is worse than one that is closed and says so.

**Nested condition combinators**, with `any:` and `all:` blocks nesting freely, as Kyverno,
Semgrep and Cloud Custodian all allow. Rejected on Semgrep's own evidence: it shipped a
structured editor because "the large search space of ways YAML-writing can go wrong threatens
to undermine the commitment to making rule writing easy". One combinator per rule, and
disjunction between two conjunctions is two rules.

**Implicit AND across a rule's keys**, as Renovate's `packageRules`. Rejected because nothing
on the page says so, and the neighbouring list form would mean OR — two invisible combinators
in adjacent lines, which is the defect the whole grammar exists to avoid.

**Reviewers as a ninth definition category.** Rejected for ADR 0004's reason: it would make
`code-review` unusable on a repo that has not rendered, which includes the repo that has just
adopted the toolkit.

## Consequences

- Rules only ever raise, so a mistaken rule costs money and never yields a shallower review
  than the context's default.
- Order carries no meaning, which is what makes a rule readable on its own. It also means a
  manifest cannot express "this rule instead of that one" — the answer is a narrower condition.
- The signal vocabulary is a fixed list a consumer cannot extend. A repo whose language the
  toolkit does not profile gets `touches` and nothing else, and adding a signal is a change to
  `agtk` rather than to that repo's manifest.
- A skill that wraps this engine does not size the review. Depth is the manifest's, and a
  wrapper that re-derived one would be a second answer to a question already answered — in
  prose, per session, against a vocabulary the binary computes and tests.
