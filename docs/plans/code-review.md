# Plan — bot code review on GitHub PRs

Status: the deterministic surface and the model-invoking pipeline are both built — the
manifest, the change profile, signals, `referencing_files`, panel selection and `agtk
code-review explain`, then the review root, the built-in prompts, reviewers at quorum,
validators, the judge and `agtk code-review run`. Findings carry a fingerprint. What is not
built is everything that talks to GitHub: posting a **Review**, reading back existing threads
to deduplicate against, and **Approval**.
Created: 2026-09-07. Challenged: 2026-09-07.

The decisions that are hard to reverse live in `docs/adr/0005-reviews-run-locally-not-in-ci.md`,
`docs/adr/0006-the-judge-decides-agtk-transmits.md`,
`docs/adr/0007-untrusted-heads-are-closed-structurally.md` and
`docs/adr/0008-the-judge-returns-ids-agtk-carries-the-evidence.md`; the vocabulary lives in
`CONTEXT.md`.

`agtk code-review` runs a panel of reviewers over a change and posts what survives to a
GitHub PR as a bot. It replaces the `deep-code-review` and `pr-code-review` skills' fan-out
with one engine, driven from Go, so a local pre-push review and a PR review are the same
machinery pointed at different targets.

The existing skills are prior art we take content from, not a baseline we preserve. The
finding schema and the axis prompts survive nearly intact; the rung ladder does not, and
`## 6` says why.

---

## 1. The flow it serves

1. Plan, implement — a session.
2. Loop review and fix locally: `agtk code-review`, driven by the skill shell.
3. Push, open the PR.
4. `agtk code-review --pr N` — a panel runs, a **Review** is posted by the App.
5. `pr-review-resolver` fetches the comments and fixes them.
6. Back to 4, when the person decides — never automatically.

Every step runs on the operator's machine against CLIs they are already logged into. The
only credential is the App's private key, and it is machine-local.

## 2. The manifest

`.agents/code-review/manifest.yaml`, beside `.agents/memory/`. A repo with no manifest uses
the embedded default; a repo with one is using it **whole**, because prompt bodies stay
shareable through `builtin:` references rather than through a merge algorithm.

The location is fixed, with no entry-manifest key to move it. `memory.root` exists because
the store is committed *content* a repo has opinions about placing; a review manifest is
configuration. Making it relocatable would buy one thing — a repo that dislikes `.agents/` —
and cost three: a name colliding with the **Review root**, a `DiagIgnoredReviewConfig` check
mirrored into the resolver's traversal, and a schema entry. Adding the key later is additive.

```yaml
version: 1

reviewers:
  unified:      {provider: claudecode, model: sonnet, prompt: builtin:unified}
  correctness:  {provider: claudecode, model: sonnet, prompt: builtin:correctness}
  security:     {provider: claudecode, model: opus,   prompt: builtin:security}
  performance:  {provider: claudecode, model: sonnet, prompt: builtin:performance}

judge:     {provider: claudecode, model: opus,   prompt: builtin:judge}
validator: {provider: claudecode, model: sonnet, prompt: builtin:validator}

panels:
  quick:    {reviewers: [unified]}
  standard: {reviewers: [correctness, security]}
  deep:     {reviewers: [correctness, security, performance], quorum: 2}

defaults:
  worktree: quick
  pr:       standard

escalate:
  - to: deep
    all:
      - touches: {matches: ["**/auth/**", "**/migrations/**"]}

  - to: deep
    any:
      - signals: {in: [concurrency, crypto, fix-revert]}

  - to: deep
    all:
      - referencing_files: {gte: 20}

  - to: standard
    all:
      - changed_files: {gte: 20}
```

A consumer names its own reviewers and its own hot paths:

```yaml
reviewers:
  architecture: {provider: codex, model: gpt-5.4, prompt: ./prompts/architecture.md}
  performance:  {provider: claudecode, model: sonnet, prompt: builtin:performance}

escalate:
  - to: deep
    all:
      - touches: {matches: ["internal/resolver/**", "internal/stack/**"]}
```

## 3. Which panel runs

The **Context** picks a default; **Escalation** rules raise it. Rules never lower it, so a
misconfigured rule can cost money and can never produce a shallower review than the default.
Every rule is evaluated and the highest target wins, so rule order carries no meaning.

`--panel deep` overrides both. `agtk code-review explain` prints the change's profile, the
default, every rule that fired, and the panel that resulted — and spends nothing, so it is
the answer to "why is this review deeper than I expected" available before paying for the
review that would tell you.

"Highest" is what a panel spends: its reviewer count times its quorum. Depth needs a total
order over panels and panels carry only names, so the order is the thing "deeper" already
meant. A declared rank would be a second thing to keep true. Equal cost is not a raise.

A condition is always `key: {operator: value}`. There is no bare form, so no combinator is
ever inferred:

| Key | Value | Operators |
|---|---|---|
| `touches` | globs over changed paths | `matches`, `not_matches` |
| `signals` | names from the built-in vocabulary | `in`, `not_in`, `all_in` |
| `changed_lines`, `changed_files` | integer, counted after mechanical exclusions | `gt`, `gte`, `lt`, `lte`, `eq` |
| `referencing_files` | integer — files referencing the symbols the change modifies | `gt`, `gte`, `lt`, `lte`, `eq` |

A count or a signal the change could not produce makes a rule reading it an error, not a
false. Three cases separate: a language with an extractor contributes its symbols; a format
that exports nothing callable — YAML, Markdown, SQL — contributes zero, which is an answer;
anything else makes the count unavailable. The table of formats that export nothing fails in
the safe direction, since a name missing from it produces a refusal rather than a count that
quietly omitted a language.

Each rule carries exactly one of `all:` or `any:`, named on the rule. A list inside an
operator means "any of", which the operator's own name forces; conjunction over a set is
`all_in`, and conjunction over globs is two members of `all:`.

Operators are words. `>=` opens a YAML folded block scalar and fails before the schema is
reached, with an error about header options that no amount of care could improve.

## 4. Signals and referencing files

Built into the binary, language-aware, listed by `agtk code-review signals`. A repo does not
declare them: a signal the toolkit cannot detect is a gap to fill upstream where it is tested
across languages, exactly as ADR 0002 argues for driver providers. A repo's own escape hatch
is `touches`, which is honest about being path-only.

`referencing_files` rides the same language table. Changed exported symbols are extracted,
then counted across the repo the way `sizing.md` counts them — with `grep`, which is that
doc's own primary method; serena is a precision upgrade behind the same key and changes
nothing a manifest can see.

A language the extractor does not know makes the count unavailable, and a manifest using
`referencing_files` is then **refused**, naming the key and the language. Unavailable is not
low. An escalation rule that silently never fires leaves a repo believing it has a protection
it does not have, which is the failure `CONTEXT.md` refuses for a **Panel** that cannot staff
itself.

## 5. The pipeline

`agtk` captures the diff and the repo's convention docs **once**, from the base ref, and
injects them into every prompt. No reviewer re-derives them and no convention summarising
run exists — docs go in raw, because summarising rules before applying them costs a run to
become less accurate.

Then: reviewers → validators → judge → `agtk` posts.

- **Reviewers** run in parallel, `quorum` instances each. Agreement between instances is the
  confidence signal.
- **Validators** are per-panel, and forced on for a **Context** that posts. A false finding
  on a PR is published and blocks **Approval**.
- **The judge** merges, re-severities and decides what survives.
- **`agtk`** posts one review, `event: COMMENT`.

Every model run carries `Request.Schema`, so findings are schema-constrained by the driver.
A run that cannot produce the shape is an unmet constraint — a verdict carrying whatever
account exists, not a parse failure to retry, and not to be read as "found nothing".

Provider capability is asserted when the manifest is read, by type assertion on the driver's
interfaces. A reviewer asking for something its provider cannot do is refused before a
process starts rather than discovered by a failed run.

## 6. Considered and rejected

**The rung ladder.** `deep-code-review` maps size class, criticality signals and reference
counts onto four rungs. It exists because a *model* invokes that skill from a session, where
nobody chose a depth. Here a person types the command having just decided the change is ready,
so the ladder infers a depth the caller already knows. Its inputs survive as escalation
conditions, where each stands alone and says what it does; the size taxonomy and the rung
arithmetic on top of them do not.

**Repo-declared content patterns.** Zones matching regexes against the diff were considered
so a repo could define `concurrency` itself. Rejected: a repo that writes `sync\.Mutex` gets
nothing the day it adds another language, and nobody goes back to update it.

**Nested condition combinators.** `any:`/`all:` blocks nesting freely, as Kyverno, Semgrep
and Cloud Custodian all allow. Rejected on Semgrep's own evidence — it shipped a structured
editor because "the large search space of ways YAML-writing can go wrong threatens to
undermine the commitment to making rule writing easy". One combinator per rule, and OR is a
second rule.

**Implicit AND across a rule's keys**, as Renovate's `packageRules`. Rejected because
nothing on the page says so, and the neighbouring list form would mean OR — two invisible
combinators in adjacent lines.

**Reviewers as a ninth definition category.** Rejected for ADR 0004's reason: it would make
`code-review` unusable on a repo that has not rendered, which includes the repo that just
adopted the toolkit.

## 7. Re-reviewing a PR that already carries one

The loop re-runs reviewers over a PR the bot has already reviewed. They re-derive what was
not fixed, which is correct, and would say it again, which is not. Nothing may accumulate
except genuinely new findings.

### What a finding is, across runs

`path` + `category` + normalized `evidence`, hashed. The finding schema already compels the
quote — "every finding must quote the offending line(s) in `evidence`. A finding without
quotable code does not get filed" — so nothing new is asked of a reviewer.

Identity is deliberately *not* the line, which moves on every push, and not the `issue`
sentence, which is generated prose and differs between two runs describing one bug. Anchoring
on the quoted code makes identity mean **this code, this kind of problem**.

Normalization takes the first non-empty line of the quote, trims it and collapses internal
whitespace. The hash is the first 12 hex characters of its sha256, the same width the memory
store uses for blob hashes.

Each posted comment carries its fingerprint as an HTML comment, invisible in rendered
markdown, so a later run looks identity up rather than re-deriving it:

```
<!-- agtk:finding v1 cdbb1d5c5dec -->
```

The `v1` is load-bearing. A change to what is hashed makes every old marker mismatch, and
without a version that reads as "all findings are new" instead of "the scheme moved".

### What each thread state does

State is read back from the PR, never cached: `reviewThreads` gives `isResolved`,
`isOutdated`, `path` and the comment bodies the fingerprints live in. The PR is what the
person actually looked at, and it survives a fresh clone.

| Existing thread with this fingerprint | Then |
|---|---|
| none | post it |
| open | do not post — it is already there and visible |
| resolved | do not post — a person read this exact code and made a call |
| outdated | post it — GitHub collapses an outdated thread, so the finding is invisible where it now lives |

Resolution is ambiguous on its own: it means *fixed* and *won't fix* equally. Evidence-based
identity separates them without a rule. A fixed finding has different code, so a different
fingerprint, so nothing suppresses it — which is what protects against a fix that did not
work. Only a finding whose quoted code is byte-identical to one somebody resolved is
suppressed, and that is the case where suppressing is right.

The **Judge** additionally receives the open threads, so it can fold a near-duplicate the hash
missed into the thread that already exists. It may drop a finding as already said; it may
never resurrect a suppressed one. Suppression is deterministic and one-directional, and the
judge's reconciliation only ever narrows what gets posted.

### Cross-cutting findings need no identity

A finding with no line — an architectural claim about a subsystem — cannot be an inline
comment, because GitHub requires a path and a line. It goes in the review body, which is a
complete statement about one commit. Nothing accumulates inside a review, and an older review
is history rather than stale current state, so there is nothing to deduplicate.

### Reviewing the same commit twice

A run whose target head already carries a bot review is a no-op that says so and exits, naming
the commit. `--force` reviews anyway. Re-reviewing an unchanged commit spends a panel to
re-derive what the PR already displays.

This is also what makes **Approval** meaningful: it requires a review for the PR's current
head, and reviews are bound to a commit. That binding is the reason the summary lives in the
review body rather than in an edited-in-place comment, which would always describe "now" and
could never be evidence that a particular commit was reviewed.

### What this does not solve

The fingerprint is exactly as stable as the model's quoting. A run that quotes one line where
the last quoted three produces a different hash and therefore a duplicate comment. The judge's
pass over open threads is the mitigation, and it is a mitigation rather than a guarantee.
Making `evidence` a single line in the reviewer prompts would tighten it, at the cost of
findings whose evidence genuinely spans a hunk.
